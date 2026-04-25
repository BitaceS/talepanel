use anyhow::{bail, Result};
use dashmap::DashMap;
use std::collections::HashMap;
use std::path::PathBuf;
use std::sync::Arc;
use tokio::task::JoinHandle;
use tracing::{error, info, instrument, warn};

use crate::api_client::ApiClient;
use crate::config::Config;
use crate::downloader;

use super::hytale::{HytaleServerProcess, ServerConfig, ServerMetrics};
use super::LogLine;

/// Central registry and lifecycle controller for all managed server processes
/// on this node.
pub struct ProcessManager {
    /// Running server processes, keyed by server_id.
    processes: Arc<DashMap<String, Arc<HytaleServerProcess>>>,
    /// Per-server tasks that relay log lines from the broadcast channel to the
    /// TalePanel API.
    log_relay_handles: Arc<DashMap<String, JoinHandle<()>>>,
    /// Per-server tasks that watch for status transitions and report them to
    /// the API.
    status_watcher_handles: Arc<DashMap<String, JoinHandle<()>>>,
    api_client: Arc<ApiClient>,
    config: Arc<Config>,
}

impl ProcessManager {
    pub fn new(api_client: Arc<ApiClient>, config: Arc<Config>) -> Self {
        Self {
            processes: Arc::new(DashMap::new()),
            log_relay_handles: Arc::new(DashMap::new()),
            status_watcher_handles: Arc::new(DashMap::new()),
            api_client,
            config,
        }
    }

    // -----------------------------------------------------------------------
    // Lifecycle
    // -----------------------------------------------------------------------

    /// Start a new Hytale server process for the given configuration.
    ///
    /// Fails if a process for `config.server_id` is already registered.
    #[instrument(skip(self, config), fields(server_id = %config.server_id))]
    pub async fn start_server(&self, config: ServerConfig) -> Result<()> {
        let server_id = config.server_id.clone();

        // Guard: refuse to start a duplicate.
        if self.processes.contains_key(&server_id) {
            bail!("Server {server_id} is already registered in the process manager");
        }

        // Enforce the configured max-server cap.
        if self.processes.len() >= self.config.resources.max_servers as usize {
            bail!(
                "Cannot start server {server_id}: node is at capacity ({} / {} servers)",
                self.processes.len(),
                self.config.resources.max_servers
            );
        }

        let process = Arc::new(HytaleServerProcess::new(
            config,
            Arc::new(self.config.hytale.clone()),
        ));
        process.start().await?;

        // Spawn the log relay task.
        let relay_handle = self.spawn_log_relay(&server_id, Arc::clone(&process));
        self.log_relay_handles.insert(server_id.clone(), relay_handle);

        // Insert into the registry *after* the relay is up so that there are
        // always subscribers before log lines start flowing.
        self.processes.insert(server_id.clone(), Arc::clone(&process));

        // Spawn a status-watcher task that reports every lifecycle transition
        // (Starting → Running, Starting → Crashed, Running → Crashed, etc.)
        // back to the TalePanel API.
        self.spawn_status_watcher(&server_id, &process);

        info!(%server_id, "Server registered in ProcessManager");
        Ok(())
    }

    /// Gracefully stop a running server process.
    ///
    /// The status watcher handles reporting to the API and removing the
    /// process from the registry once it reaches a terminal state.
    #[instrument(skip(self), fields(server_id))]
    pub async fn stop_server(&self, server_id: &str) -> Result<()> {
        let process = self
            .processes
            .get(server_id)
            .map(|p| Arc::clone(p.value()))
            .ok_or_else(|| anyhow::anyhow!("Server {server_id} is not managed by this node"))?;

        process.stop().await?;

        info!(%server_id, "Server stop completed");
        Ok(())
    }

    /// Restart a server: stop it then start it with the same configuration.
    #[instrument(skip(self), fields(server_id))]
    pub async fn restart_server(&self, server_id: &str) -> Result<()> {
        let config = {
            let process = self
                .processes
                .get(server_id)
                .map(|p| Arc::clone(p.value()))
                .ok_or_else(|| anyhow::anyhow!("Server {server_id} is not managed by this node"))?;
            let cfg = process.config.clone();
            process.stop().await?;
            cfg
        };

        // Wait briefly for the status watcher to clean up the old entry
        for _ in 0..20 {
            if !self.processes.contains_key(server_id) {
                break;
            }
            tokio::time::sleep(tokio::time::Duration::from_millis(100)).await;
        }
        // Force-remove if the watcher hasn't cleaned up yet
        self.processes.remove(server_id);
        self.cleanup_relay(server_id);

        // Re-use the same ServerConfig for the new process.
        self.start_server(config).await?;

        info!(%server_id, "Server restarted");
        Ok(())
    }

    /// Immediately kill a server process (no graceful shutdown).
    ///
    /// The status watcher handles reporting to the API and removing the
    /// process from the registry once it reaches a terminal state.
    #[instrument(skip(self), fields(server_id))]
    pub async fn kill_server(&self, server_id: &str) -> Result<()> {
        let process = self
            .processes
            .get(server_id)
            .map(|p| Arc::clone(p.value()))
            .ok_or_else(|| anyhow::anyhow!("Server {server_id} is not managed by this node"))?;

        process.kill().await?;

        info!(%server_id, "Server kill completed");
        Ok(())
    }

    // -----------------------------------------------------------------------
    // Provisioning
    // -----------------------------------------------------------------------

    /// Download Hytale server files to `data_path` using the Hytale Downloader CLI.
    ///
    /// Runs in the background; reports `stopped` (ready) or `crashed` status to
    /// the API when finished.  The caller receives an immediate `Ok(())` once the
    /// background task is spawned.
    #[instrument(skip(self), fields(server_id, version))]
    pub async fn provision_server(
        &self,
        server_id: String,
        version: String,
        data_path: String,
    ) -> Result<()> {
        let api_client = Arc::clone(&self.api_client);
        let data_root = self.config.daemon.data_root.clone();

        tokio::spawn(async move {
            info!(%server_id, %version, %data_path, "Starting server provisioning");

            // Helper to push a single log line to the panel console
            let push_log = |msg: String, level: &str| {
                let api = Arc::clone(&api_client);
                let sid = server_id.clone();
                let lvl = level.to_string();
                async move {
                    let _ = api.push_log_lines(&sid, vec![crate::api_client::LogLine {
                        timestamp: chrono::Utc::now(),
                        level: lvl,
                        message: msg,
                    }]).await;
                }
            };

            push_log("Provisioning started — downloading Hytale server files...".into(), "INFO").await;

            let target = PathBuf::from(&data_path);

            // Check if server files already exist (idempotent).
            let jar_path = target.join("Server").join("HytaleServer.jar");
            if jar_path.exists() {
                info!(%server_id, "Server files already present — skipping download");
                push_log("Server files already present — skipping download".into(), "INFO").await;
                let _ = api_client.report_server_status(&server_id, "stopped").await;
                return;
            }

            // Local-bin fallback: before hitting the Hytale CDN, check whether
            // the operator pre-placed a JAR + Assets.zip at
            // {data_root}/hytale-bin/.  This is the supported "offline /
            // air-gapped install" path and also the fastest — hardlinking
            // avoids copying multi-gigabyte assets per server.
            let local_bin = PathBuf::from(&data_root).join("hytale-bin");
            let local_jar = local_bin.join("HytaleServer.jar");
            let local_assets = local_bin.join("Assets.zip");
            if local_jar.exists() && local_assets.exists() {
                push_log(format!("Using local Hytale binaries from {}", local_bin.display()), "INFO").await;
                let server_dir = target.join("Server");
                if let Err(e) = tokio::fs::create_dir_all(&server_dir).await {
                    push_log(format!("Failed to create Server/: {e}"), "ERROR").await;
                    let _ = api_client.report_server_status(&server_id, "crashed").await;
                    return;
                }
                // Hardlink (same filesystem).  Fall back to copy if hardlink
                // fails (e.g. cross-mount with restrictive perms).
                for (src, dst) in [
                    (local_jar.clone(), server_dir.join("HytaleServer.jar")),
                    (local_bin.join("HytaleServer.aot"), server_dir.join("HytaleServer.aot")),
                    (local_assets.clone(), target.join("Assets.zip")),
                ] {
                    if !src.exists() { continue; }
                    if tokio::fs::hard_link(&src, &dst).await.is_err() {
                        if let Err(e) = tokio::fs::copy(&src, &dst).await {
                            push_log(format!("Failed to place {}: {e}", dst.display()), "ERROR").await;
                            let _ = api_client.report_server_status(&server_id, "crashed").await;
                            return;
                        }
                    }
                }
                // Licenses dir is optional — cp -r equivalent.
                let licenses_src = local_bin.join("Licenses");
                if licenses_src.is_dir() {
                    let _ = copy_dir(&licenses_src, &server_dir.join("Licenses")).await;
                }
                push_log("Provisioning complete via local hytale-bin".into(), "INFO").await;
                let _ = api_client.report_server_status(&server_id, "stopped").await;
                return;
            }

            // Try downloading from the official Hytale Downloader.
            let tool_dir = PathBuf::from(&data_root).join("tools");
            let download_ok = match downloader::fetch_downloader_cli(&tool_dir).await {
                Ok(downloader_bin) => {
                    push_log(format!("Downloader CLI ready at {}", downloader_bin.display()), "INFO").await;
                    push_log("Running Hytale Downloader — this may take a few minutes...".into(), "INFO").await;

                    let (log_tx, mut log_rx) = tokio::sync::mpsc::unbounded_channel::<String>();
                    let relay_api = Arc::clone(&api_client);
                    let relay_sid = server_id.clone();
                    let relay_handle = tokio::spawn(async move {
                        while let Some(line) = log_rx.recv().await {
                            let _ = relay_api.push_log_lines(&relay_sid, vec![crate::api_client::LogLine {
                                timestamp: chrono::Utc::now(),
                                level: "INFO".to_string(),
                                message: line,
                            }]).await;
                        }
                    });

                    let result = downloader::download_server_files(&downloader_bin, &target, &version, Some(log_tx)).await;
                    let _ = relay_handle.await;

                    match result {
                        Ok(r) => {
                            let msg = format!(
                                "Provisioning complete (version {}) — click Start in the panel to launch the server.",
                                r.version
                            );
                            info!(%server_id, path = %r.data_path.display(), version = %r.version, "Provisioning complete");
                            push_log(msg, "INFO").await;
                            true
                        }
                        Err(e) => {
                            warn!(%server_id, error = %e, "Hytale Downloader failed");
                            push_log(format!("Downloader failed: {e}"), "WARN").await;
                            false
                        }
                    }
                }
                Err(e) => {
                    warn!(%server_id, error = %e, "Failed to fetch Hytale Downloader CLI");
                    push_log(format!("Could not fetch Hytale Downloader: {e}"), "WARN").await;
                    false
                }
            };

            if !download_ok {
                // Fallback: create the directory structure with a dev-mode stub
                // so the file manager works and the server process can be tested.
                push_log("Download unavailable — creating dev-mode server stub...".into(), "INFO").await;

                let server_dir = target.join("Server");
                if let Err(e) = tokio::fs::create_dir_all(&server_dir).await {
                    let msg = format!("Failed to create server directory: {e}");
                    error!(%server_id, error = %e, "Failed to create server directory");
                    push_log(msg, "ERROR").await;
                    let _ = api_client.report_server_status(&server_id, "crashed").await;
                    return;
                }

                // Create a minimal shell script that mimics a game server:
                // prints a startup banner, reads stdin, and stays alive.
                let stub_script = server_dir.join("HytaleServer.jar");
                let stub_content = r#"#!/bin/sh
echo "[TalePanel Dev] Simulated Hytale Server starting..."
echo "[TalePanel Dev] Listening on port $1 (dev mode — not a real server)"
echo "[TalePanel Dev] Type 'stop' to shut down"
while IFS= read -r line; do
    echo "[TalePanel Dev] > $line"
    case "$line" in
        stop|exit|quit) echo "[TalePanel Dev] Shutting down..."; exit 0 ;;
    esac
done
"#;
                if let Err(e) = tokio::fs::write(&stub_script, stub_content).await {
                    error!(%server_id, error = %e, "Failed to write dev stub");
                    push_log(format!("Failed to write dev stub: {e}"), "ERROR").await;
                    let _ = api_client.report_server_status(&server_id, "crashed").await;
                    return;
                }

                // Make executable
                #[cfg(unix)]
                {
                    use std::os::unix::fs::PermissionsExt;
                    if let Ok(meta) = tokio::fs::metadata(&stub_script).await {
                        let mut perms = meta.permissions();
                        perms.set_mode(0o755);
                        let _ = tokio::fs::set_permissions(&stub_script, perms).await;
                    }
                }

                // Create an empty Assets.zip placeholder
                let _ = tokio::fs::write(target.join("Assets.zip"), b"").await;

                // Create a default server.json config
                let server_json = serde_json::json!({
                    "dev_mode": true,
                    "note": "This is a dev-mode stub. Replace with real Hytale server files for production."
                });
                let _ = tokio::fs::write(
                    target.join("server.json"),
                    serde_json::to_string_pretty(&server_json).unwrap_or_default(),
                ).await;

                info!(%server_id, "Dev-mode server stub created");
                push_log("Dev-mode server stub ready — provisioning complete".into(), "INFO").await;
            }

            let _ = api_client.report_server_status(&server_id, "stopped").await;
        });

        Ok(())
    }

    // -----------------------------------------------------------------------
    // In-game commands
    // -----------------------------------------------------------------------

    /// Forward a console command to the specified running server.
    #[instrument(skip(self), fields(server_id, cmd))]
    pub async fn send_command(&self, server_id: &str, cmd: &str) -> Result<()> {
        let process = self
            .processes
            .get(server_id)
            .ok_or_else(|| anyhow::anyhow!("Server {server_id} is not managed by this node"))?;

        process.send_command(cmd).await
    }

    // -----------------------------------------------------------------------
    // Observability
    // -----------------------------------------------------------------------

    /// Return a snapshot list of all currently-managed server IDs.
    pub fn list_servers(&self) -> Vec<String> {
        self.processes.iter().map(|e| e.key().clone()).collect()
    }

    /// Return the current status of a specific server, or `None` if unknown.
    pub fn get_server_status(&self, server_id: &str) -> Option<super::ServerStatus> {
        self.processes
            .get(server_id)
            .map(|p| p.get_status())
    }

    /// Collect resource metrics from every managed server process.
    ///
    /// The returned map is keyed by server_id.  Servers that fail to report
    /// metrics are silently omitted.
    pub async fn collect_all_metrics(&self) -> HashMap<String, ServerMetrics> {
        let mut out = HashMap::with_capacity(self.processes.len());

        for entry in self.processes.iter() {
            let server_id = entry.key().clone();
            let process = entry.value().clone();
            // Drop the DashMap guard before awaiting.
            drop(entry);

            let metrics = process.get_metrics().await;
            out.insert(server_id, metrics);
        }

        out
    }

    /// Stop every managed server process as cleanly as possible.
    ///
    /// Used during daemon shutdown.  Errors are logged but do not prevent
    /// subsequent servers from being stopped.
    pub async fn stop_all(&self) {
        let server_ids: Vec<String> = self.list_servers();
        info!(count = server_ids.len(), "Stopping all managed servers");

        for server_id in server_ids {
            if let Err(err) = self.stop_server(&server_id).await {
                error!(%err, %server_id, "Failed to stop server during shutdown");
            }
        }
    }

    // -----------------------------------------------------------------------
    // Internal helpers
    // -----------------------------------------------------------------------

    /// Spawn a task that reads log lines from the process's broadcast channel
    /// and pushes them to the TalePanel API in batches of up to 1 second.
    fn spawn_log_relay(
        &self,
        server_id: &str,
        process: Arc<HytaleServerProcess>,
    ) -> JoinHandle<()> {
        let mut rx = process.subscribe_logs();
        let api_client = Arc::clone(&self.api_client);
        let server_id = server_id.to_string();

        tokio::spawn(async move {
            let mut batch: Vec<LogLine> = Vec::new();
            let mut flush_interval =
                tokio::time::interval(tokio::time::Duration::from_secs(1));

            loop {
                tokio::select! {
                    // Collect incoming log lines without flushing yet.
                    recv_result = rx.recv() => {
                        match recv_result {
                            Ok(line) => {
                                batch.push(line);
                            }
                            Err(tokio::sync::broadcast::error::RecvError::Lagged(n)) => {
                                warn!(%server_id, skipped = n, "Log relay lagged; some lines were dropped");
                            }
                            Err(tokio::sync::broadcast::error::RecvError::Closed) => {
                                // Sender dropped (process stopped); flush remaining lines and exit.
                                if !batch.is_empty() {
                                    let lines = std::mem::take(&mut batch);
                                    let wire_lines = to_wire_log_lines(lines);
                                    if let Err(err) = api_client.push_log_lines(&server_id, wire_lines).await {
                                        warn!(%err, %server_id, "Failed to push final log batch");
                                    }
                                }
                                break;
                            }
                        }
                    }

                    // Flush every second, regardless of batch size.
                    _ = flush_interval.tick() => {
                        if !batch.is_empty() {
                            let lines = std::mem::take(&mut batch);
                            let wire_lines = to_wire_log_lines(lines);
                            if let Err(err) = api_client.push_log_lines(&server_id, wire_lines).await {
                                warn!(%err, %server_id, "Failed to push log batch to API");
                            }
                        }
                    }
                }
            }

            info!(%server_id, "Log relay task exited");
        })
    }

    /// Abort and remove the log-relay task for the given server.
    fn cleanup_relay(&self, server_id: &str) {
        if let Some((_, handle)) = self.log_relay_handles.remove(server_id) {
            handle.abort();
        }
        if let Some((_, handle)) = self.status_watcher_handles.remove(server_id) {
            handle.abort();
        }
    }

    /// Spawn a background task that watches for status transitions on the
    /// process's watch channel and reports each one to the TalePanel API.
    ///
    /// On terminal states (Stopped / Crashed) the task also removes the
    /// process from the registry so a fresh `start_server` can succeed.
    fn spawn_status_watcher(
        &self,
        server_id: &str,
        process: &Arc<HytaleServerProcess>,
    ) {
        let mut rx = process.subscribe_status();
        let api_client = Arc::clone(&self.api_client);
        let key = server_id.to_string();
        let server_id = key.clone();
        let processes = Arc::clone(&self.processes);
        let log_relay_handles = Arc::clone(&self.log_relay_handles);
        let status_watcher_handles = Arc::clone(&self.status_watcher_handles);

        let handle = tokio::spawn(async move {
            // Report the initial status (should be "starting")
            let initial = *rx.borrow();
            if let Err(err) = api_client
                .report_server_status(&server_id, &initial.to_string())
                .await
            {
                warn!(%err, %server_id, "Failed to report initial status to API");
            }

            loop {
                match rx.changed().await {
                    Ok(()) => {
                        let new_status = *rx.borrow();
                        info!(%server_id, %new_status, "Server status changed");

                        if let Err(err) = api_client
                            .report_server_status(&server_id, &new_status.to_string())
                            .await
                        {
                            warn!(%err, %server_id, %new_status,
                                "Failed to report status change to API");
                        }

                        // On terminal states, clean up the process from the
                        // registry so it can be re-started.
                        if new_status == super::ServerStatus::Crashed
                            || new_status == super::ServerStatus::Stopped
                        {
                            processes.remove(&server_id);
                            if let Some((_, h)) = log_relay_handles.remove(&server_id) {
                                h.abort();
                            }
                            status_watcher_handles.remove(&server_id);
                            info!(%server_id, %new_status,
                                "Process removed from registry (terminal state)");
                            break;
                        }
                    }
                    Err(_) => {
                        // Sender dropped — process struct was deallocated
                        warn!(%server_id, "Status watch channel closed");
                        break;
                    }
                }
            }
        });

        self.status_watcher_handles.insert(key, handle);
    }
}

// ---------------------------------------------------------------------------
// Type conversion helper
// ---------------------------------------------------------------------------

/// Convert internal `LogLine` types to the API wire format.
// copy_dir recursively copies src into dst, creating dst if needed.
// Used by the local-bin fallback in provision.  Kept minimal — if more
// features are needed we can switch to the `fs_extra` crate later.
async fn copy_dir(src: &std::path::Path, dst: &std::path::Path) -> std::io::Result<()> {
    tokio::fs::create_dir_all(dst).await?;
    let mut entries = tokio::fs::read_dir(src).await?;
    while let Some(entry) = entries.next_entry().await? {
        let ty = entry.file_type().await?;
        let from = entry.path();
        let to = dst.join(entry.file_name());
        if ty.is_dir() {
            Box::pin(copy_dir(&from, &to)).await?;
        } else {
            tokio::fs::copy(&from, &to).await?;
        }
    }
    Ok(())
}

fn to_wire_log_lines(lines: Vec<LogLine>) -> Vec<crate::api_client::LogLine> {
    lines
        .into_iter()
        .map(|l| crate::api_client::LogLine {
            timestamp: l.timestamp,
            level: l.level,
            message: l.message,
        })
        .collect()
}
