/// Real Hytale server process manager.
///
/// Launches the Hytale dedicated server JAR via the system Java runtime,
/// captures its stdout/stderr, feeds log lines to the broadcast channel,
/// and manages the full process lifecycle (start → running → stop/kill/crash).
///
/// Layout expected inside `config.data_path`:
///   {data_path}/
///     Server/
///       HytaleServer.jar   ← main server JAR (or custom jar_name in config)
///     Assets.zip           ← required by the runtime
///     server.json          ← Hytale server config (managed by TalePanel)
use std::sync::atomic::{AtomicBool, AtomicU32, Ordering};
use std::sync::Arc;
use std::time::Instant;

use anyhow::{bail, Context, Result};
use serde::{Deserialize, Serialize};
use tokio::io::{AsyncBufReadExt, AsyncWriteExt, BufReader};
use tokio::process::{Child, Command};
use tokio::sync::{watch, Mutex, RwLock};
use tracing::{debug, error, info, instrument, warn};

use crate::config::HytaleConfig;
use super::{LogLine, ServerStatus};

// ─────────────────────────────────────────────────────────────────────────────
// ServerConfig
// ─────────────────────────────────────────────────────────────────────────────

/// Per-server launch configuration provided by the TalePanel API.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ServerConfig {
    /// UUID matching the TalePanel `servers.id` record.
    pub server_id: String,
    /// Human-readable name (used only for logging).
    pub name: String,
    /// Absolute path to this server's data directory on the host.
    /// Expected layout: `{data_path}/Server/HytaleServer.jar`
    pub data_path: String,
    /// UDP port the game server binds to for player connections.
    pub port: u16,
    /// Maximum heap size in MB passed as `-Xmx{n}M` to the JVM.
    /// 0 = no limit (not recommended).
    pub ram_limit_mb: u32,
    /// Fractional CPU limit (informational; enforced by cgroups, not JVM).
    pub cpu_limit: f32,
    /// Maximum number of automatic crash-restarts before giving up.
    pub crash_limit: u32,
}

// ─────────────────────────────────────────────────────────────────────────────
// ServerMetrics
// ─────────────────────────────────────────────────────────────────────────────

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct ServerMetrics {
    pub cpu_percent: f32,
    pub ram_mb: u64,
    pub uptime_s: u64,
}

// ─────────────────────────────────────────────────────────────────────────────
// HytaleServerProcess
// ─────────────────────────────────────────────────────────────────────────────

/// Wraps a live Hytale server child process.
pub struct HytaleServerProcess {
    pub server_id: String,
    pub config: ServerConfig,

    /// Current lifecycle status.  Use the watch channel so callers can
    /// `.await` a status transition without polling.
    status_tx: Arc<watch::Sender<ServerStatus>>,
    status_rx: watch::Receiver<ServerStatus>,

    /// Broadcast channel for log lines (capacity 2048).
    pub log_tx: tokio::sync::broadcast::Sender<LogLine>,

    /// Wall-clock instant the last `start()` call succeeded.
    start_time: Arc<RwLock<Option<Instant>>>,

    /// OS PID of the running child process (0 = not running).
    pid: Arc<AtomicU32>,

    /// Set to `true` before a graceful stop so the exit-watcher task knows
    /// the termination was intentional and should not report a crash.
    stop_requested: Arc<AtomicBool>,

    /// Stdin writer shared between `start()` and `send_command()`.
    /// Wrapped in `Option` so `start()` can take ownership of the real stdin
    /// and replace it; `None` when the server is not running.
    stdin: Arc<Mutex<Option<tokio::io::BufWriter<tokio::process::ChildStdin>>>>,

    /// Crash counter — reset by the ProcessManager when a restart succeeds
    /// within the cooldown window.
    pub crash_count: Arc<AtomicU32>,

    /// Daemon-level Hytale configuration (jar path, java binary, timeouts).
    hytale_cfg: Arc<HytaleConfig>,
}

impl HytaleServerProcess {
    pub fn new(config: ServerConfig, hytale_cfg: Arc<HytaleConfig>) -> Self {
        let (log_tx, _) = tokio::sync::broadcast::channel(2048);
        let (status_tx, status_rx) = watch::channel(ServerStatus::Stopped);

        Self {
            server_id: config.server_id.clone(),
            config,
            status_tx: Arc::new(status_tx),
            status_rx,
            log_tx,
            start_time: Arc::new(RwLock::new(None)),
            pid: Arc::new(AtomicU32::new(0)),
            stop_requested: Arc::new(AtomicBool::new(false)),
            stdin: Arc::new(Mutex::new(None)),
            crash_count: Arc::new(AtomicU32::new(0)),
            hytale_cfg,
        }
    }

    // ─────────────────────────────────────────────────────────────────────────
    // Status helpers
    // ─────────────────────────────────────────────────────────────────────────

    pub fn get_status(&self) -> ServerStatus {
        *self.status_rx.borrow()
    }

    /// Wait until the server reaches `target` status or `timeout` elapses.
    pub async fn wait_for_status(
        &self,
        target: ServerStatus,
        timeout: std::time::Duration,
    ) -> Result<()> {
        let mut rx = self.status_tx.subscribe();
        let deadline = tokio::time::sleep(timeout);
        tokio::pin!(deadline);

        loop {
            tokio::select! {
                _ = &mut deadline => {
                    bail!(
                        "Timed out waiting for server {} to reach status {:?}",
                        self.server_id, target
                    );
                }
                result = rx.changed() => {
                    result.context("Status watch channel closed")?;
                    if *rx.borrow() == target {
                        return Ok(());
                    }
                }
            }
        }
    }

    fn set_status(&self, s: ServerStatus) {
        self.status_tx.send_replace(s);
    }

    // ─────────────────────────────────────────────────────────────────────────
    // Lifecycle: start
    // ─────────────────────────────────────────────────────────────────────────

    /// Spawn the Hytale server JAR and begin streaming its output.
    #[instrument(skip(self), fields(server_id = %self.server_id))]
    pub async fn start(&self) -> Result<()> {
        let current = self.get_status();
        if current != ServerStatus::Stopped && current != ServerStatus::Crashed {
            bail!(
                "Cannot start server {}: current status is {:?}",
                self.server_id, current
            );
        }

        self.stop_requested.store(false, Ordering::SeqCst);
        self.set_status(ServerStatus::Starting);
        *self.start_time.write().await = Some(Instant::now());

        // ── Build the server command ─────────────────────────────────────────
        let jar_path = std::path::Path::new(&self.config.data_path)
            .join(&self.hytale_cfg.server_jar);

        if !jar_path.exists() {
            self.set_status(ServerStatus::Stopped);
            bail!(
                "Server JAR not found at {}: run the downloader first",
                jar_path.display()
            );
        }

        // Detect dev-mode stub: if the "JAR" starts with `#!` it's a shell
        // script created by the provisioner when the Hytale Downloader is
        // unavailable.  Run it with sh instead of java.
        let is_dev_stub = match tokio::fs::read(&jar_path).await {
            Ok(bytes) => bytes.starts_with(b"#!"),
            Err(_) => false,
        };

        let mut cmd = if is_dev_stub {
            info!(server_id = %self.server_id, "Using dev-mode stub (not a real JAR)");
            let mut c = Command::new("sh");
            c.arg(&jar_path);
            c.arg(format!("{}", self.config.port));
            c
        } else {
            let mut c = Command::new(&self.hytale_cfg.java_binary);

            // JVM memory flags (match Pterodactyl egg: -Xms128M -Xmx${RAM}M)
            c.arg("-Xms128M");
            if self.config.ram_limit_mb > 0 {
                c.arg(format!("-Xmx{}M", self.config.ram_limit_mb));
            }

            // Extra JVM args from config (e.g. GC tuning, AOT cache)
            for arg in &self.hytale_cfg.extra_jvm_args {
                c.arg(arg);
            }

            // Main JAR
            c.arg("-jar").arg(&jar_path);

            // Hytale server arguments (matching official Pterodactyl egg format)
            c.arg("--auth-mode").arg("AUTHENTICATED");
            c.arg("--assets").arg("Assets.zip");
            c.arg("--bind").arg(format!("0.0.0.0:{}", self.config.port));

            // Extra server args from config
            for arg in &self.hytale_cfg.extra_server_args {
                c.arg(arg);
            }
            c
        };

        cmd.current_dir(&self.config.data_path)
            .stdin(std::process::Stdio::piped())
            .stdout(std::process::Stdio::piped())
            .stderr(std::process::Stdio::piped())
            // Do NOT kill on drop — we manage shutdown explicitly.
            .kill_on_drop(false);

        let mut child = cmd.spawn().context("Failed to spawn Hytale server process")?;

        // ── Capture PID ───────────────────────────────────────────────────────
        let pid = child.id().unwrap_or(0);
        self.pid.store(pid, Ordering::SeqCst);
        info!(server_id = %self.server_id, pid, "Hytale server process spawned");

        // ── Extract I/O handles ───────────────────────────────────────────────
        let stdin = child
            .stdin
            .take()
            .context("Failed to capture server stdin")?;
        let stdout = child
            .stdout
            .take()
            .context("Failed to capture server stdout")?;
        let stderr = child
            .stderr
            .take()
            .context("Failed to capture server stderr")?;

        // Store stdin for later command injection
        *self.stdin.lock().await = Some(tokio::io::BufWriter::new(stdin));

        // ── Clone Arcs for background tasks ───────────────────────────────────
        let status_tx = Arc::clone(&self.status_tx);
        let log_tx_out = self.log_tx.clone();
        let log_tx_err = self.log_tx.clone();
        let log_tx_exit = self.log_tx.clone();
        let server_id = self.server_id.clone();
        let stop_requested = Arc::clone(&self.stop_requested);
        let start_time = Arc::clone(&self.start_time);
        let stdin_arc = Arc::clone(&self.stdin);
        let startup_timeout = self.hytale_cfg.startup_timeout_s;

        // ── Stdout reader task ────────────────────────────────────────────────
        let server_id_out = server_id.clone();
        let status_tx_out = Arc::clone(&status_tx);
        tokio::spawn(async move {
            let reader = BufReader::new(stdout);
            let mut lines = reader.lines();

            while let Ok(Some(line)) = lines.next_line().await {
                let level = classify_level(&line);
                debug!(server_id = %server_id_out, %line, "stdout");

                // Detect server-ready signals in stdout
                if *status_tx_out.borrow() == ServerStatus::Starting {
                    if is_ready_signal(&line) {
                        info!(server_id = %server_id_out, "Server ready signal detected");
                        status_tx_out.send_replace(ServerStatus::Running);
                    }
                }

                let _ = log_tx_out.send(LogLine {
                    timestamp: chrono::Utc::now(),
                    level,
                    message: line,
                });
            }
        });

        // ── Stderr reader task ────────────────────────────────────────────────
        let server_id_err = server_id.clone();
        tokio::spawn(async move {
            let reader = BufReader::new(stderr);
            let mut lines = reader.lines();

            while let Ok(Some(line)) = lines.next_line().await {
                debug!(server_id = %server_id_err, %line, "stderr");
                let _ = log_tx_err.send(LogLine {
                    timestamp: chrono::Utc::now(),
                    level: "ERROR".to_string(),
                    message: line,
                });
            }
        });

        // ── Startup timeout watchdog ──────────────────────────────────────────
        // If the server doesn't reach Running within the configured timeout,
        // report a startup failure.
        let status_tx_wd = Arc::clone(&status_tx);
        let server_id_wd = server_id.clone();
        let log_tx_wd = self.log_tx.clone();
        tokio::spawn(async move {
            tokio::time::sleep(tokio::time::Duration::from_secs(startup_timeout)).await;
            if *status_tx_wd.borrow() == ServerStatus::Starting {
                error!(server_id = %server_id_wd, "Server startup timed out");
                let _ = log_tx_wd.send(LogLine {
                    timestamp: chrono::Utc::now(),
                    level: "ERROR".to_string(),
                    message: format!(
                        "Server did not reach ready state within {}s — check your JAR and Assets.zip",
                        startup_timeout
                    ),
                });
                status_tx_wd.send_replace(ServerStatus::Crashed);
            }
        });

        // ── Exit-watcher task ─────────────────────────────────────────────────
        let server_id_exit = server_id.clone();
        let status_tx_exit = Arc::clone(&status_tx);
        tokio::spawn(async move {
            // child is moved here and we await its natural exit
            Self::watch_exit(
                child,
                server_id_exit,
                status_tx_exit,
                log_tx_exit,
                stop_requested,
                start_time,
                stdin_arc,
            )
            .await;
        });

        Ok(())
    }

    /// Awaits child process exit and updates status accordingly.
    async fn watch_exit(
        mut child: Child,
        server_id: String,
        status_tx: Arc<watch::Sender<ServerStatus>>,
        log_tx: tokio::sync::broadcast::Sender<LogLine>,
        stop_requested: Arc<AtomicBool>,
        start_time: Arc<RwLock<Option<Instant>>>,
        stdin: Arc<Mutex<Option<tokio::io::BufWriter<tokio::process::ChildStdin>>>>,
    ) {
        match child.wait().await {
            Ok(exit_status) => {
                // Drop stdin so the process can exit cleanly
                drop(stdin.lock().await.take());

                if stop_requested.load(Ordering::SeqCst) {
                    info!(server_id = %server_id, "Server exited after stop request");
                    status_tx.send_replace(ServerStatus::Stopped);
                    let _ = log_tx.send(LogLine {
                        timestamp: chrono::Utc::now(),
                        level: "INFO".to_string(),
                        message: "Server stopped gracefully".to_string(),
                    });
                } else {
                    let code = exit_status.code().unwrap_or(-1);
                    error!(server_id = %server_id, exit_code = code, "Server exited unexpectedly");
                    let _ = log_tx.send(LogLine {
                        timestamp: chrono::Utc::now(),
                        level: "ERROR".to_string(),
                        message: format!(
                            "Server process exited with code {code} — marking as crashed"
                        ),
                    });
                    status_tx.send_replace(ServerStatus::Crashed);
                }
            }
            Err(e) => {
                error!(server_id = %server_id, error = %e, "Error waiting for server process");
                status_tx.send_replace(ServerStatus::Crashed);
            }
        }

        // Clear start time on exit
        *start_time.write().await = None;
    }

    // ─────────────────────────────────────────────────────────────────────────
    // Lifecycle: stop
    // ─────────────────────────────────────────────────────────────────────────

    /// Gracefully stop the server: send the `stop` console command, wait up to
    /// `stop_timeout_s`, then SIGKILL if it doesn't exit in time.
    #[instrument(skip(self), fields(server_id = %self.server_id))]
    pub async fn stop(&self) -> Result<()> {
        if self.get_status() == ServerStatus::Stopped {
            return Ok(());
        }

        self.stop_requested.store(true, Ordering::SeqCst);
        self.set_status(ServerStatus::Stopping);

        info!(server_id = %self.server_id, "Sending stop command to server");

        // Hytale server listens for "stop" on stdin (same pattern as Minecraft)
        if let Err(e) = self.write_stdin("stop").await {
            warn!(server_id = %self.server_id, error = %e, "Failed to send stop via stdin — will force kill");
        }

        // Wait for the exit-watcher to transition to Stopped
        let timeout = std::time::Duration::from_secs(self.hytale_cfg.stop_timeout_s);
        match self
            .wait_for_status(ServerStatus::Stopped, timeout)
            .await
        {
            Ok(_) => {
                info!(server_id = %self.server_id, "Server stopped gracefully");
            }
            Err(_) => {
                warn!(
                    server_id = %self.server_id,
                    "Server did not stop within {}s — force killing",
                    self.hytale_cfg.stop_timeout_s
                );
                self.force_kill().await?;
            }
        }

        Ok(())
    }

    // ─────────────────────────────────────────────────────────────────────────
    // Lifecycle: kill
    // ─────────────────────────────────────────────────────────────────────────

    /// Immediately terminate the server process (SIGKILL / TerminateProcess).
    #[instrument(skip(self), fields(server_id = %self.server_id))]
    pub async fn kill(&self) -> Result<()> {
        self.stop_requested.store(true, Ordering::SeqCst);
        self.set_status(ServerStatus::Stopping);
        self.force_kill().await?;
        info!(server_id = %self.server_id, "Server process killed");
        Ok(())
    }

    async fn force_kill(&self) -> Result<()> {
        let pid = self.pid.load(Ordering::SeqCst);
        if pid == 0 {
            self.set_status(ServerStatus::Stopped);
            return Ok(());
        }

        kill_pid(pid);

        // Give the OS up to 3 seconds to reap the process before we declare it stopped
        let _ = self
            .wait_for_status(
                ServerStatus::Stopped,
                std::time::Duration::from_secs(3),
            )
            .await;

        // If the exit-watcher hasn't fired yet (race), set Stopped manually
        if self.get_status() != ServerStatus::Stopped {
            self.set_status(ServerStatus::Stopped);
        }

        Ok(())
    }

    // ─────────────────────────────────────────────────────────────────────────
    // Console commands
    // ─────────────────────────────────────────────────────────────────────────

    /// Write a command line to the server's stdin.
    /// The Hytale server reads commands from stdin exactly like Minecraft.
    #[instrument(skip(self), fields(server_id = %self.server_id, cmd))]
    pub async fn send_command(&self, cmd: &str) -> Result<()> {
        let status = self.get_status();
        if status != ServerStatus::Running {
            bail!(
                "Cannot send command to server {}: status is {:?}",
                self.server_id, status
            );
        }

        self.write_stdin(cmd).await?;

        // Echo the command back into the log stream so it shows in the panel console
        let _ = self.log_tx.send(LogLine {
            timestamp: chrono::Utc::now(),
            level: "CMD".to_string(),
            message: format!("> {cmd}"),
        });

        Ok(())
    }

    async fn write_stdin(&self, line: &str) -> Result<()> {
        let mut guard = self.stdin.lock().await;
        let writer = guard
            .as_mut()
            .context("Server stdin is not available (server not running)")?;

        writer
            .write_all(format!("{line}\n").as_bytes())
            .await
            .context("Failed to write to server stdin")?;
        writer.flush().await.context("Failed to flush server stdin")?;
        Ok(())
    }

    // ─────────────────────────────────────────────────────────────────────────
    // Observability
    // ─────────────────────────────────────────────────────────────────────────

    pub fn subscribe_logs(&self) -> tokio::sync::broadcast::Receiver<LogLine> {
        self.log_tx.subscribe()
    }

    /// Subscribe to status transitions via the watch channel.
    pub fn subscribe_status(&self) -> watch::Receiver<ServerStatus> {
        self.status_tx.subscribe()
    }

    /// Collect real resource metrics for this process using the `sysinfo` crate.
    pub async fn get_metrics(&self) -> ServerMetrics {
        let pid = self.pid.load(Ordering::SeqCst);
        let uptime_s = self
            .start_time
            .read()
            .await
            .map(|t| t.elapsed().as_secs())
            .unwrap_or(0);

        if pid == 0 {
            return ServerMetrics { cpu_percent: 0.0, ram_mb: 0, uptime_s };
        }

        // sysinfo process query — runs on the calling thread (blocking but fast)
        let (cpu_percent, ram_mb) = tokio::task::spawn_blocking(move || {
            use sysinfo::{Pid, ProcessRefreshKind, RefreshKind, System};
            let mut sys = System::new_with_specifics(
                RefreshKind::new().with_processes(ProcessRefreshKind::new().with_cpu().with_memory()),
            );
            let sysinfo_pid = Pid::from(pid as usize);
            sys.refresh_process_specifics(sysinfo_pid, ProcessRefreshKind::new().with_cpu().with_memory());

            if let Some(proc) = sys.process(sysinfo_pid) {
                let cpu = proc.cpu_usage();
                let ram = proc.memory() / 1024 / 1024; // bytes → MB
                (cpu, ram)
            } else {
                (0.0, 0)
            }
        })
        .await
        .unwrap_or((0.0, 0));

        ServerMetrics { cpu_percent, ram_mb, uptime_s }
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

/// Classify a log line level from its text content.
fn classify_level(line: &str) -> String {
    let upper = line.to_ascii_uppercase();
    if upper.contains("[ERROR]") || upper.contains("ERROR:") || upper.contains("EXCEPTION") || upper.contains("FATAL") {
        "ERROR".to_string()
    } else if upper.contains("[WARN]") || upper.contains("WARNING") || upper.contains("WARN:") {
        "WARN".to_string()
    } else {
        "INFO".to_string()
    }
}

/// Returns `true` if the log line signals that the server is fully started
/// and accepting player connections.
///
/// Pattern list is intentionally broad — we match several common phrases
/// so it still works across minor Hytale server version changes.
fn is_ready_signal(line: &str) -> bool {
    let l = line.to_ascii_lowercase();
    l.contains("server started")
        || l.contains("server booted")
        || l.contains("done (")
        || l.contains("accepting connections")
        || l.contains("listening on port")
        || l.contains("server is ready")
        || l.contains("ready for connections")
        || l.contains("started serving")
}

/// Kill a process by PID using the OS-appropriate mechanism.
fn kill_pid(pid: u32) {
    if pid == 0 {
        return;
    }

    #[cfg(unix)]
    {
        // Send SIGKILL directly via libc — no external process needed.
        unsafe {
            libc::kill(pid as libc::pid_t, libc::SIGKILL);
        }
        info!(pid, "Sent SIGKILL to Hytale server process");
    }

    #[cfg(windows)]
    {
        use std::process::Command;
        let _ = Command::new("taskkill")
            .args(["/F", "/PID", &pid.to_string()])
            .output();
        info!(pid, "Sent TerminateProcess (taskkill /F) to Hytale server process");
    }
}
