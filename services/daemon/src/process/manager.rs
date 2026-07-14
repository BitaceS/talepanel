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
        let dev_stub_allowed = self.config.dev_stub_allowed();

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

            if !download_ok && !dev_stub_allowed {
                // Hard fail. Never fabricate a server: a stub that "runs" but
                // hosts nothing is a lie the operator only discovers when
                // players cannot connect.
                let msg = format!(
                    "Provisioning FAILED: could not obtain the Hytale server files. \
                     TalePanel will not create a fake server. \
                     Fix: place HytaleServer.jar and Assets.zip (plus the optional \
                     HytaleServer.aot and Licenses/) into {}/hytale-bin/ on this node \
                     and press Reinstall — the daemon will pick them up automatically. \
                     Alternatively make the Hytale Downloader reachable from this node.",
                    data_root
                );
                error!(%server_id, "Provisioning failed and dev stub is not allowed");
                push_log(msg, "ERROR").await;
                let _ = api_client.report_server_status(&server_id, "crashed").await;
                return;
            }

            if !download_ok {
                // Explicitly opted in via allow_dev_stub (non-production only):
                // create the directory structure with a dev-mode stub so the file
                // manager works and the server process can be tested.
                warn!(%server_id, "Creating dev-mode server stub (allow_dev_stub = true) — this is NOT a real Hytale server");
                push_log(
                    "Download unavailable — creating dev-mode server stub (allow_dev_stub=true). \
                     THIS IS NOT A REAL HYTALE SERVER: it accepts console input and nothing else."
                        .into(),
                    "WARN",
                )
                .await;

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
                push_log("Dev-mode server stub ready — NOT a real Hytale server".into(), "WARN").await;
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

        // Player events must reach the API in the order the log emitted them.
        // Spawning a task per event does not guarantee that: on a reconnect the
        // `join` request can overtake the stalled `leave`, and the API then
        // closes the session the join just opened — the player stays online with
        // no open session and their whole next session's playtime is lost.
        //
        // So they go through one ordered queue with a single consumer. The relay
        // loop stays non-blocking (send on an unbounded channel does not await),
        // but the HTTP calls happen strictly in sequence.
        let (event_tx, mut event_rx) =
            tokio::sync::mpsc::unbounded_channel::<(&'static str, String, String)>();
        {
            let api = Arc::clone(&api_client);
            let sid = server_id.clone();
            tokio::spawn(async move {
                while let Some((action, username, hytale_uuid)) = event_rx.recv().await {
                    if let Err(err) = api.report_player_event(&sid, action, &username, &hytale_uuid).await {
                        warn!(server_id = %sid, %action, %username, error = %err,
                            "Failed to report player event");
                    }
                }
            });
        }

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
                                // Detect player join/leave and report them so the
                                // panel's player list stays populated.
                                if let Some((action, username, hytale_uuid)) = parse_player_event(&line.message) {
                                    // Receiver lives as long as this relay task; a
                                    // send error means the server is going away.
                                    let _ = event_tx.send((action, username, hytale_uuid));
                                }
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

/// Parse a Hytale server log line for a player join/leave event.
/// Join lines look like:  `... Adding player 'NAME (UUID)`
/// Leave lines look like: `... Removing player 'NAME' (UUID)`
/// Returns (action, username, hytale_uuid) with action "join" or "leave".
fn parse_player_event(raw: &str) -> Option<(&'static str, String, String)> {
    let line = strip_ansi(raw);

    // Only the "[Universe|P] Adding player 'NAME (UUID)" line is the canonical
    // join. Other lines like "[World|x] Adding player 'NAME' to world ..." also
    // contain "Adding player '" but are not clean join events — the username
    // validity check below rejects them (they carry spaces/quotes).
    if let Some(pos) = line.find("Adding player '") {
        let rest = &line[pos + "Adding player '".len()..];
        if let Some(op) = rest.find(" (") {
            let name = rest[..op].trim().to_string();
            let after = &rest[op + 2..];
            if let Some(cp) = after.find(')') {
                let id = after[..cp].trim().to_string();
                if is_valid_username(&name) && is_uuid(&id) {
                    return Some(("join", name, id));
                }
            }
        }
    }

    if let Some(pos) = line.find("Removing player '") {
        let rest = &line[pos + "Removing player '".len()..];
        if let Some(q) = rest.find('\'') {
            let name = rest[..q].trim().to_string();
            let after = &rest[q..];
            if let Some(op) = after.find('(') {
                let after2 = &after[op + 1..];
                if let Some(cp) = after2.find(')') {
                    let id = after2[..cp].trim().to_string();
                    if is_valid_username(&name) && is_uuid(&id) {
                        return Some(("leave", name, id));
                    }
                }
            }
        }
    }

    None
}

/// True if `s` looks like a Hytale username (alphanumeric or underscore,
/// 1..=32 chars). Rejects log fragments that leak into the join/leave lines.
fn is_valid_username(s: &str) -> bool {
    !s.is_empty()
        && s.len() <= 32
        && s.bytes().all(|b| b.is_ascii_alphanumeric() || b == b'_')
}

/// True if `s` is a canonical 8-4-4-4-12 hex UUID.
fn is_uuid(s: &str) -> bool {
    s.len() == 36
        && s.as_bytes().iter().enumerate().all(|(i, &b)| {
            if i == 8 || i == 13 || i == 18 || i == 23 {
                b == b'-'
            } else {
                b.is_ascii_hexdigit()
            }
        })
}

/// Strip ANSI CSI escape sequences (colour codes) from a log line.
fn strip_ansi(s: &str) -> String {
    let mut out = String::with_capacity(s.len());
    let mut chars = s.chars().peekable();
    while let Some(c) = chars.next() {
        if c == '\u{1b}' {
            if chars.peek() == Some(&'[') {
                chars.next();
                while let Some(nc) = chars.next() {
                    if nc.is_ascii_alphabetic() {
                        break;
                    }
                }
            }
        } else {
            out.push(c);
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    const UUID: &str = "550e8400-e29b-41d4-a716-446655440000";

    #[test]
    fn parses_a_join_line() {
        let line = format!("12:03:41 [Universe|P] Adding player 'Steve ({UUID})");
        let (action, name, id) = parse_player_event(&line).expect("join not parsed");
        assert_eq!(action, "join");
        assert_eq!(name, "Steve");
        assert_eq!(id, UUID);
    }

    #[test]
    fn parses_a_leave_line() {
        let line = format!("12:44:02 [Universe|P] Removing player 'Steve' ({UUID})");
        let (action, name, id) = parse_player_event(&line).expect("leave not parsed");
        assert_eq!(action, "leave");
        assert_eq!(name, "Steve");
        assert_eq!(id, UUID);
    }

    #[test]
    fn parses_through_ansi_colour_codes() {
        let line = format!("\u{1b}[32m[Universe|P]\u{1b}[0m Adding player 'Alex_2 ({UUID})");
        let (action, name, _) = parse_player_event(&line).expect("coloured join not parsed");
        assert_eq!(action, "join");
        assert_eq!(name, "Alex_2");
    }

    /// The world-level line also contains "Adding player '" but is not a join.
    /// Only the username check keeps it out — if that check ever loosens, this
    /// test is what catches the resulting phantom players.
    #[test]
    fn ignores_the_world_level_adding_player_line() {
        let line = format!("[World|main] Adding player 'Steve' to world 'overworld' ({UUID})");
        assert!(parse_player_event(&line).is_none());
    }

    #[test]
    fn rejects_a_malformed_uuid() {
        let line = "[Universe|P] Adding player 'Steve (not-a-uuid)";
        assert!(parse_player_event(line).is_none());
    }

    #[test]
    fn ignores_an_ordinary_log_line() {
        assert!(parse_player_event("[Server] Done (4.21s)! For help, type 'help'").is_none());
    }
}
