use std::sync::Arc;
use tokio::sync::broadcast;
use tracing::{info, instrument, warn};

use crate::api_client::{ApiClient, CommandResult};
use crate::config::Config;
use crate::process::{manager::ProcessManager, hytale::ServerConfig};

/// Run the command-polling loop until a shutdown signal is received.
///
/// Every `config.resources.command_poll_interval_s` seconds this function:
///   1. Calls `api_client.get_pending_commands()`.
///   2. Executes each command via the appropriate `ProcessManager` method.
///   3. Acknowledges each command with success/failure via `api_client.ack_command()`.
///
/// Poll failures are logged at WARN level but do not stop the loop.
#[instrument(skip_all)]
pub async fn run_command_poller(
    api_client: Arc<ApiClient>,
    process_manager: Arc<ProcessManager>,
    config: Arc<Config>,
    mut shutdown: broadcast::Receiver<()>,
) {
    let interval_duration =
        tokio::time::Duration::from_secs(config.resources.command_poll_interval_s);
    let mut interval = tokio::time::interval(interval_duration);
    interval.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);

    info!(
        poll_interval_s = config.resources.command_poll_interval_s,
        "Command poller started"
    );

    loop {
        tokio::select! {
            _ = interval.tick() => {
                poll_and_dispatch(&api_client, &process_manager).await;
            }

            _ = shutdown.recv() => {
                info!("Command poller received shutdown signal; exiting");
                break;
            }
        }
    }
}

/// Fetch pending commands and dispatch each one, then acknowledge.
async fn poll_and_dispatch(
    api_client: &Arc<ApiClient>,
    process_manager: &Arc<ProcessManager>,
) {
    let commands = match api_client.get_pending_commands().await {
        Ok(cmds) => cmds,
        Err(err) => {
            warn!(%err, "Command poll failed");
            return;
        }
    };

    for command in commands {

        let command_id = command.id.clone();
        let server_id = command.server_id.clone();
        let command_type = command.command_type.as_str();

        let result = match command_type {
            "start" => {
                // The payload must deserialise into a `ServerConfig`.
                match serde_json::from_value::<ServerConfig>(command.payload.clone()) {
                    Ok(server_cfg) => {
                        match process_manager.start_server(server_cfg).await {
                            Ok(()) => CommandResult::ok(format!("Server {server_id} started")),
                            Err(err) => {
                                warn!(%err, %server_id, "Failed to start server");
                                CommandResult::err(err.to_string())
                            }
                        }
                    }
                    Err(err) => {
                        warn!(
                            %err,
                            command_id = %command_id,
                            "Failed to deserialise ServerConfig from start command payload"
                        );
                        CommandResult::err(format!("Invalid start payload: {err}"))
                    }
                }
            }

            "stop" => match process_manager.stop_server(&server_id).await {
                Ok(()) => CommandResult::ok(format!("Server {server_id} stopped")),
                Err(err) => {
                    warn!(%err, %server_id, "Failed to stop server");
                    CommandResult::err(err.to_string())
                }
            },

            "restart" => match process_manager.restart_server(&server_id).await {
                Ok(()) => CommandResult::ok(format!("Server {server_id} restarted")),
                Err(err) => {
                    warn!(%err, %server_id, "Failed to restart server");
                    CommandResult::err(err.to_string())
                }
            },

            "kill" => match process_manager.kill_server(&server_id).await {
                Ok(()) => CommandResult::ok(format!("Server {server_id} killed")),
                Err(err) => {
                    warn!(%err, %server_id, "Failed to kill server");
                    CommandResult::err(err.to_string())
                }
            },

            "send_command" => {
                // The payload should contain a "cmd" string field.
                let cmd = command
                    .payload
                    .get("cmd")
                    .and_then(|v| v.as_str())
                    .unwrap_or("")
                    .to_string();

                if cmd.is_empty() {
                    CommandResult::err(
                        "send_command payload missing required 'cmd' string field".to_string(),
                    )
                } else {
                    match process_manager.send_command(&server_id, &cmd).await {
                        Ok(()) => CommandResult::ok(format!("Command sent to {server_id}: {cmd}")),
                        Err(err) => {
                            warn!(%err, %server_id, %cmd, "Failed to send command to server");
                            CommandResult::err(err.to_string())
                        }
                    }
                }
            }

            "install_mod" => {
                let data_path = command.payload.get("data_path").and_then(|v| v.as_str()).unwrap_or("").to_string();
                let filename = command.payload.get("filename").and_then(|v| v.as_str()).unwrap_or("").to_string();
                let download_url = command.payload.get("download_url").and_then(|v| v.as_str()).unwrap_or("").to_string();

                if data_path.is_empty() || filename.is_empty() || download_url.is_empty() {
                    CommandResult::err("install_mod payload missing required fields".to_string())
                } else if !is_safe_mod_filename(&filename) {
                    CommandResult::err(format!("install_mod: unsafe filename {filename}"))
                } else if let Err(e) = validate_download_url(&download_url).await {
                    CommandResult::err(format!("install_mod: {e}"))
                } else {
                    match download_and_install_mod(&download_url, &data_path, &filename).await {
                        Ok(()) => {
                            info!(%filename, "Mod installed");
                            CommandResult::ok(format!("Installed mod {filename}"))
                        }
                        Err(err) => {
                            warn!(%err, %filename, "Failed to install mod");
                            CommandResult::err(err.to_string())
                        }
                    }
                }
            }

            "remove_mod" => {
                let data_path = command.payload.get("data_path").and_then(|v| v.as_str()).unwrap_or("").to_string();
                let filename = command.payload.get("filename").and_then(|v| v.as_str()).unwrap_or("").to_string();

                if data_path.is_empty() || filename.is_empty() {
                    CommandResult::err("remove_mod payload missing required fields".to_string())
                } else if !is_safe_mod_filename(&filename) {
                    CommandResult::err(format!("remove_mod: unsafe filename {filename}"))
                } else {
                    let path = format!("{data_path}/mods/{filename}");
                    match tokio::fs::remove_file(&path).await {
                        Ok(()) => {
                            info!(%filename, "Mod removed");
                            CommandResult::ok(format!("Removed mod {filename}"))
                        }
                        Err(err) => {
                            warn!(%err, %path, "Failed to remove mod");
                            CommandResult::err(err.to_string())
                        }
                    }
                }
            }

            "enable_mod" => {
                let data_path = command.payload.get("data_path").and_then(|v| v.as_str()).unwrap_or("").to_string();
                let filename = command.payload.get("filename").and_then(|v| v.as_str()).unwrap_or("").to_string();

                if data_path.is_empty() || filename.is_empty() {
                    CommandResult::err("enable_mod payload missing required fields".to_string())
                } else if !is_safe_mod_filename(&filename) {
                    CommandResult::err(format!("enable_mod: unsafe filename {filename}"))
                } else {
                    let disabled = format!("{data_path}/mods/{filename}.disabled");
                    let enabled  = format!("{data_path}/mods/{filename}");
                    if std::path::Path::new(&disabled).exists() {
                        match std::fs::rename(&disabled, &enabled) {
                            Ok(()) => {
                                tracing::info!("enabled mod: {}", filename);
                                CommandResult::ok(format!("Enabled mod {filename}"))
                            }
                            Err(err) => {
                                warn!(%err, %filename, "enable_mod rename failed");
                                CommandResult::err(format!("enable_mod rename failed: {err}"))
                            }
                        }
                    } else if std::path::Path::new(&enabled).exists() {
                        tracing::info!("enable_mod: {} already enabled, no-op", filename);
                        CommandResult::ok(format!("Mod {filename} already enabled"))
                    } else {
                        tracing::warn!("enable_mod: neither {} nor {}.disabled found", filename, filename);
                        CommandResult::err(format!("enable_mod: {filename} not found"))
                    }
                }
            }

            "disable_mod" => {
                let data_path = command.payload.get("data_path").and_then(|v| v.as_str()).unwrap_or("").to_string();
                let filename = command.payload.get("filename").and_then(|v| v.as_str()).unwrap_or("").to_string();

                if data_path.is_empty() || filename.is_empty() {
                    CommandResult::err("disable_mod payload missing required fields".to_string())
                } else if !is_safe_mod_filename(&filename) {
                    CommandResult::err(format!("disable_mod: unsafe filename {filename}"))
                } else {
                    let enabled  = format!("{data_path}/mods/{filename}");
                    let disabled = format!("{data_path}/mods/{filename}.disabled");
                    if std::path::Path::new(&enabled).exists() {
                        match std::fs::rename(&enabled, &disabled) {
                            Ok(()) => {
                                tracing::info!("disabled mod: {}", filename);
                                CommandResult::ok(format!("Disabled mod {filename}"))
                            }
                            Err(err) => {
                                warn!(%err, %filename, "disable_mod rename failed");
                                CommandResult::err(format!("disable_mod rename failed: {err}"))
                            }
                        }
                    } else if std::path::Path::new(&disabled).exists() {
                        tracing::info!("disable_mod: {} already disabled, no-op", filename);
                        CommandResult::ok(format!("Mod {filename} already disabled"))
                    } else {
                        tracing::warn!("disable_mod: {} not found", filename);
                        CommandResult::err(format!("disable_mod: {filename} not found"))
                    }
                }
            }

            unknown => {
                warn!(
                    command_type = %unknown,
                    %command_id,
                    %server_id,
                    "Received unknown command type; skipping"
                );
                CommandResult::err(format!("Unknown command type: {unknown}"))
            }
        };

        // Always acknowledge, even on failure, so the API does not retry
        // indefinitely for commands the daemon does not support.
        if let Err(err) = api_client.ack_command(&command_id, result).await {
            warn!(%err, %command_id, "Failed to acknowledge command");
        }
    }
}

/// Download a .jar file from `url` and save it to `{data_path}/mods/{filename}`.
async fn download_and_install_mod(url: &str, data_path: &str, filename: &str) -> anyhow::Result<()> {
    use tokio::io::AsyncWriteExt;

    let mods_dir = format!("{data_path}/mods");
    tokio::fs::create_dir_all(&mods_dir).await?;

    let client = reqwest::Client::new();
    let resp = client.get(url).send().await?;
    if !resp.status().is_success() {
        anyhow::bail!("Download failed: HTTP {}", resp.status());
    }

    let bytes = resp.bytes().await?;
    let file_path = format!("{mods_dir}/{filename}");
    let mut file = tokio::fs::File::create(&file_path).await?;
    file.write_all(&bytes).await?;

    Ok(())
}

/// Reject mod filenames that could escape the mods directory. Only a bare
/// basename is permitted — no path separators, parent refs, or NUL bytes —
/// so `{data_path}/mods/{filename}` cannot be steered elsewhere on the node.
fn is_safe_mod_filename(name: &str) -> bool {
    !name.is_empty()
        && name != "."
        && name != ".."
        && !name.contains('/')
        && !name.contains('\\')
        && !name.contains("..")
        && !name.contains('\0')
}

/// Returns true for addresses a mod download must never reach: loopback,
/// private, link-local (incl. the 169.254.169.254 cloud-metadata endpoint),
/// and unspecified ranges. Blocks SSRF into the node's internal network.
fn is_disallowed_ip(ip: &std::net::IpAddr) -> bool {
    match ip {
        std::net::IpAddr::V4(v4) => {
            v4.is_private()
                || v4.is_loopback()
                || v4.is_link_local()
                || v4.is_broadcast()
                || v4.is_unspecified()
                || v4.octets()[0] == 0
        }
        std::net::IpAddr::V6(v6) => v6.is_loopback() || v6.is_unspecified(),
    }
}

/// Validate a mod download URL before fetching it. Requires https and resolves
/// the host, rejecting any address inside the node's trust boundary. This is a
/// best-effort SSRF guard (a rebinding attack could still race between this
/// check and the fetch); pairing it with an allowlist is recommended.
async fn validate_download_url(raw: &str) -> Result<(), String> {
    let url = reqwest::Url::parse(raw).map_err(|e| format!("invalid url: {e}"))?;
    if url.scheme() != "https" {
        return Err("only https download URLs are allowed".to_string());
    }
    let host = url
        .host_str()
        .ok_or_else(|| "url has no host".to_string())?
        .to_lowercase();
    if host == "localhost" || host.ends_with(".localhost") || host.ends_with(".internal") {
        return Err("download host not allowed".to_string());
    }
    let port = url.port_or_known_default().unwrap_or(443);
    let mut resolved_any = false;
    match tokio::net::lookup_host((host.as_str(), port)).await {
        Ok(addrs) => {
            for addr in addrs {
                resolved_any = true;
                if is_disallowed_ip(&addr.ip()) {
                    return Err("download host resolves to a disallowed address".to_string());
                }
            }
        }
        Err(e) => return Err(format!("could not resolve download host: {e}")),
    }
    if !resolved_any {
        return Err("download host did not resolve".to_string());
    }
    Ok(())
}
