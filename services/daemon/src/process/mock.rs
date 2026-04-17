// TODO: Replace this entire module with real Hytale server process management
//       once the Hytale dedicated-server binary is available.
//
//       The real implementation should:
//         1. Spawn the binary via tokio::process::Command
//         2. Attach stdout/stderr readers to feed the broadcast channel
//         3. Use tokio::process::Child::kill() / SIGTERM (Unix) for stop/kill
//         4. Write to Child stdin for send_command()
//         5. Read CPU/RAM from /proc/{pid}/stat and /proc/{pid}/status (Linux)
//            or use the `sysinfo` crate for cross-platform support

use anyhow::{bail, Result};
use rand::Rng;
use serde::{Deserialize, Serialize};
use std::sync::Arc;
use std::time::Instant;
use tokio::sync::{broadcast, RwLock};
use tracing::{debug, info, instrument, warn};

use super::{LogLine, ServerStatus};

// ---------------------------------------------------------------------------
// ServerConfig
// ---------------------------------------------------------------------------

/// Configuration for a single Hytale server instance.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ServerConfig {
    /// Unique identifier matching the TalePanel server record.
    pub server_id: String,
    /// Human-readable display name.
    pub name: String,
    /// Absolute path to this server's data directory on the host filesystem.
    pub data_path: String,
    /// UDP port the server will bind to for player connections.
    pub port: u16,
    /// Maximum RAM the server process is allowed to use, in megabytes.
    pub ram_limit_mb: u32,
    /// Fractional CPU limit (e.g., 2.0 = two full cores).
    pub cpu_limit: f32,
}

// ---------------------------------------------------------------------------
// ServerMetrics
// ---------------------------------------------------------------------------

/// Point-in-time resource metrics for a single server process.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ServerMetrics {
    /// CPU utilisation of this process as a percentage (0.0–100.0).
    pub cpu_percent: f32,
    /// RAM currently used by this process, in megabytes.
    pub ram_mb: u64,
    /// Seconds elapsed since the server was started.
    pub uptime_s: u64,
}

// ---------------------------------------------------------------------------
// MockServerProcess
// ---------------------------------------------------------------------------

/// A simulated Hytale server process used for MVP development and testing.
///
/// # TODO
/// Replace with a struct that wraps `tokio::process::Child` once the real
/// Hytale dedicated-server binary is available.
pub struct MockServerProcess {
    pub server_id: String,
    pub status: Arc<RwLock<ServerStatus>>,
    /// Broadcast channel for log lines; subscribers receive a clone of every
    /// line emitted by the simulated server.  Channel capacity is 1024 lines.
    pub log_tx: broadcast::Sender<LogLine>,
    /// Wall-clock instant at which `start()` was called; `None` if the server
    /// has never been started.
    pub start_time: Arc<RwLock<Option<Instant>>>,
    pub config: ServerConfig,
}

impl MockServerProcess {
    /// Construct a new (stopped) mock process for the given server config.
    pub fn new(config: ServerConfig) -> Self {
        let (log_tx, _) = broadcast::channel(1024);
        Self {
            server_id: config.server_id.clone(),
            status: Arc::new(RwLock::new(ServerStatus::Stopped)),
            log_tx,
            start_time: Arc::new(RwLock::new(None)),
            config,
        }
    }

    // -----------------------------------------------------------------------
    // Lifecycle
    // -----------------------------------------------------------------------

    /// Start the mock server process.
    ///
    /// # TODO
    /// Replace the body of this method with:
    /// ```ignore
    /// let mut child = tokio::process::Command::new("/opt/hytale/server")
    ///     .arg("--port").arg(self.config.port.to_string())
    ///     .arg("--data").arg(&self.config.data_path)
    ///     .stdout(Stdio::piped())
    ///     .stderr(Stdio::piped())
    ///     .spawn()?;
    /// // Attach readers to self.log_tx …
    /// ```
    #[instrument(skip(self), fields(server_id = %self.server_id))]
    pub async fn start(&self) -> Result<()> {
        {
            let current = self.status.read().await;
            if *current != ServerStatus::Stopped && *current != ServerStatus::Crashed {
                bail!(
                    "Cannot start server {}: current status is {}",
                    self.server_id,
                    *current
                );
            }
        }

        // Transition to Starting.
        *self.status.write().await = ServerStatus::Starting;
        *self.start_time.write().await = Some(Instant::now());

        info!(server_id = %self.server_id, "Mock server process started");

        // Clone shared state into the background simulation task.
        let status = Arc::clone(&self.status);
        let log_tx = self.log_tx.clone();
        let server_id = self.server_id.clone();

        tokio::spawn(async move {
            // TODO: In the real implementation, await the child process's
            //       readiness signal (e.g., a "Server started" line on stdout)
            //       rather than sleeping for a fixed duration.
            tokio::time::sleep(tokio::time::Duration::from_secs(2)).await;

            // Transition to Running.
            *status.write().await = ServerStatus::Running;
            let _ = log_tx.send(LogLine::info("Server started and accepting connections"));

            // Simulate periodic server log output.
            let mut tick: u64 = 0;
            loop {
                tokio::time::sleep(tokio::time::Duration::from_secs(3)).await;

                let current_status = *status.read().await;
                if current_status == ServerStatus::Stopping
                    || current_status == ServerStatus::Stopped
                    || current_status == ServerStatus::Crashed
                {
                    break;
                }

                tick += 1;
                let line = match tick % 5 {
                    0 => LogLine::info("Server tick 20tps"),
                    1 => LogLine::info("Player count: 0"),
                    2 => LogLine::info(format!("World save completed (tick {tick})")),
                    3 => LogLine::info("Chunk GC: reclaimed 0 chunks"),
                    _ => LogLine::info(format!("Heartbeat #{tick} – all systems nominal")),
                };

                if log_tx.send(line).is_err() {
                    // No active subscribers; that is fine.
                    debug!(server_id = %server_id, "No log subscribers; discarding line");
                }
            }

            info!(server_id = %server_id, "Mock log loop exited");
        });

        Ok(())
    }

    /// Gracefully stop the mock server process.
    ///
    /// # TODO
    /// Replace with:
    /// ```ignore
    /// child.kill(); // or send SIGTERM on Unix
    /// child.wait().await?;
    /// ```
    #[instrument(skip(self), fields(server_id = %self.server_id))]
    pub async fn stop(&self) -> Result<()> {
        {
            let current = self.status.read().await;
            if *current == ServerStatus::Stopped {
                return Ok(()); // Already stopped; idempotent.
            }
        }

        *self.status.write().await = ServerStatus::Stopping;

        // TODO: Await real process termination instead of sleeping.
        tokio::time::sleep(tokio::time::Duration::from_secs(1)).await;

        *self.status.write().await = ServerStatus::Stopped;
        let _ = self
            .log_tx
            .send(LogLine::info("Server stopped gracefully"));

        info!(server_id = %self.server_id, "Mock server process stopped");
        Ok(())
    }

    /// Immediately terminate the mock server process (no graceful shutdown).
    ///
    /// # TODO
    /// Replace with `child.kill()` / SIGKILL.
    #[instrument(skip(self), fields(server_id = %self.server_id))]
    pub async fn kill(&self) -> Result<()> {
        *self.status.write().await = ServerStatus::Stopped;
        let _ = self.log_tx.send(LogLine::warn("Server process killed (SIGKILL)"));
        warn!(server_id = %self.server_id, "Mock server process killed");
        Ok(())
    }

    // -----------------------------------------------------------------------
    // In-game commands
    // -----------------------------------------------------------------------

    /// Send a console command to the running server.
    ///
    /// # TODO
    /// Replace with a write to the child process's stdin:
    /// ```ignore
    /// child.stdin.write_all(format!("{cmd}\n").as_bytes()).await?;
    /// ```
    #[instrument(skip(self), fields(server_id = %self.server_id, cmd))]
    pub async fn send_command(&self, cmd: &str) -> Result<()> {
        let current = *self.status.read().await;
        if current != ServerStatus::Running {
            bail!(
                "Cannot send command to server {}: not running (status: {})",
                self.server_id,
                current
            );
        }

        let _ = self
            .log_tx
            .send(LogLine::info(format!("CMD > {cmd}")));

        debug!(server_id = %self.server_id, cmd, "Mock command sent to server");
        Ok(())
    }

    // -----------------------------------------------------------------------
    // Observability
    // -----------------------------------------------------------------------

    /// Subscribe to the server's log broadcast channel.
    ///
    /// Each subscriber receives every `LogLine` emitted after the point of
    /// subscription.  Slow subscribers will miss lines if the channel buffer
    /// (1024) fills up.
    pub fn subscribe_logs(&self) -> broadcast::Receiver<LogLine> {
        self.log_tx.subscribe()
    }

    /// Return the current lifecycle status of this server process.
    pub fn get_status(&self) -> ServerStatus {
        // We need a synchronous view; use `try_read` and fall back to a
        // conservative value rather than blocking.
        self.status
            .try_read()
            .map(|g| *g)
            .unwrap_or(ServerStatus::Starting)
    }

    /// Collect resource-usage metrics for this server process.
    ///
    /// # TODO
    /// Replace with real metrics from `/proc/{pid}/stat` (Linux) or the
    /// `sysinfo` crate:
    /// ```ignore
    /// let mut sys = sysinfo::System::new();
    /// sys.refresh_process(pid);
    /// let proc = sys.process(pid).unwrap();
    /// ServerMetrics { cpu_percent: proc.cpu_usage(), ram_mb: proc.memory() / 1024 / 1024, … }
    /// ```
    pub async fn get_metrics(&self) -> ServerMetrics {
        let mut rng = rand::thread_rng();

        // TODO: Read from /proc/{pid}/stat for CPU, /proc/{pid}/status for RAM.
        let cpu_percent: f32 = rng.gen_range(5.0_f32..30.0_f32);
        let ram_mb: u64 = rng.gen_range(256_u64..1024_u64);

        let uptime_s = self
            .start_time
            .read()
            .await
            .map(|t| t.elapsed().as_secs())
            .unwrap_or(0);

        ServerMetrics {
            cpu_percent,
            ram_mb,
            uptime_s,
        }
    }
}
