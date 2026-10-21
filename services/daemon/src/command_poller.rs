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

    // Same layout the process manager provisions servers into.
    let servers_dir = format!("{}/servers", config.daemon.data_root.trim_end_matches('/'));

    loop {
        tokio::select! {
            _ = interval.tick() => {
                poll_and_dispatch(&api_client, &process_manager, &servers_dir).await;
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
    servers_dir: &str,
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

        // The API is not trusted. Names carried in payloads are validated per
        // command, but `data_path` is a *path*, and delete_world hands it to
        // remove_dir_all — a payload with data_path = "/" would delete
        // /universe/worlds/<name>. The API derives data_path server-side today,
        // so this is defence in depth, not a live hole; it is also the only
        // place the daemon accepts a path from the outside, which is exactly
        // where the next mistake would land. So it must be exactly the
        // directory of the server the command is for: {servers_dir}/{server_id}.
        if let Some(dp) = command.payload.get("data_path").and_then(|v| v.as_str()) {
            if !is_server_data_path(dp, servers_dir, &server_id) {
                warn!(%server_id, %command_type, data_path = %dp,
                    "Rejected command: data_path is not this server's directory");
                let result = CommandResult::err(format!(
                    "{command_type}: data_path {dp} is not {servers_dir}/{server_id}"
                ));
                if let Err(err) = api_client.ack_command(&command_id, result).await {
                    warn!(%command_id, %err, "Failed to ack rejected command");
                }
                continue;
            }
        }

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
                } else {
                    match download_and_install_mod(&download_url, &data_path, &filename).await {
                        Ok(()) => {
                            info!(%filename, "Mod installed");
                            CommandResult::ok(format!("Installed mod {filename}"))
                        }
                        Err(err) => {
                            warn!(%err, %filename, "Failed to install mod");
                            CommandResult::err(format!("install_mod: {err}"))
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

            // `enable_mod` / `disable_mod` also serve plugins: the optional
            // "dir" field selects the target directory ("mods" | "plugins").
            // Omitting it keeps the historic behaviour (mods/), so panels that
            // predate plugin toggling keep working.
            "enable_mod" => {
                let data_path = command.payload.get("data_path").and_then(|v| v.as_str()).unwrap_or("").to_string();
                let filename = command.payload.get("filename").and_then(|v| v.as_str()).unwrap_or("").to_string();
                let dir = command.payload.get("dir").and_then(|v| v.as_str()).unwrap_or(DEFAULT_MOD_DIR);

                if data_path.is_empty() || filename.is_empty() {
                    CommandResult::err("enable_mod payload missing required fields".to_string())
                } else if !is_safe_mod_filename(&filename) {
                    CommandResult::err(format!("enable_mod: unsafe filename {filename}"))
                } else if !is_allowed_mod_dir(dir) {
                    CommandResult::err(format!("enable_mod: unsupported dir {dir}"))
                } else {
                    let disabled = format!("{data_path}/{dir}/{filename}.disabled");
                    let enabled  = format!("{data_path}/{dir}/{filename}");
                    if std::path::Path::new(&disabled).exists() {
                        match std::fs::rename(&disabled, &enabled) {
                            Ok(()) => {
                                tracing::info!("enabled mod: {}/{}", dir, filename);
                                CommandResult::ok(format!("Enabled {filename} in {dir}/"))
                            }
                            Err(err) => {
                                warn!(%err, %filename, %dir, "enable_mod rename failed");
                                CommandResult::err(format!("enable_mod rename failed: {err}"))
                            }
                        }
                    } else if std::path::Path::new(&enabled).exists() {
                        tracing::info!("enable_mod: {}/{} already enabled, no-op", dir, filename);
                        CommandResult::ok(format!("{filename} already enabled"))
                    } else {
                        tracing::warn!("enable_mod: neither {0}/{1} nor {0}/{1}.disabled found", dir, filename);
                        CommandResult::err(format!("enable_mod: {filename} not found in {dir}/"))
                    }
                }
            }

            "disable_mod" => {
                let data_path = command.payload.get("data_path").and_then(|v| v.as_str()).unwrap_or("").to_string();
                let filename = command.payload.get("filename").and_then(|v| v.as_str()).unwrap_or("").to_string();
                let dir = command.payload.get("dir").and_then(|v| v.as_str()).unwrap_or(DEFAULT_MOD_DIR);

                if data_path.is_empty() || filename.is_empty() {
                    CommandResult::err("disable_mod payload missing required fields".to_string())
                } else if !is_safe_mod_filename(&filename) {
                    CommandResult::err(format!("disable_mod: unsafe filename {filename}"))
                } else if !is_allowed_mod_dir(dir) {
                    CommandResult::err(format!("disable_mod: unsupported dir {dir}"))
                } else {
                    let enabled  = format!("{data_path}/{dir}/{filename}");
                    let disabled = format!("{data_path}/{dir}/{filename}.disabled");
                    if std::path::Path::new(&enabled).exists() {
                        match std::fs::rename(&enabled, &disabled) {
                            Ok(()) => {
                                tracing::info!("disabled mod: {}/{}", dir, filename);
                                CommandResult::ok(format!("Disabled {filename} in {dir}/"))
                            }
                            Err(err) => {
                                warn!(%err, %filename, %dir, "disable_mod rename failed");
                                CommandResult::err(format!("disable_mod rename failed: {err}"))
                            }
                        }
                    } else if std::path::Path::new(&disabled).exists() {
                        tracing::info!("disable_mod: {}/{} already disabled, no-op", dir, filename);
                        CommandResult::ok(format!("{filename} already disabled"))
                    } else {
                        tracing::warn!("disable_mod: {}/{} not found", dir, filename);
                        CommandResult::err(format!("disable_mod: {filename} not found in {dir}/"))
                    }
                }
            }

            // ── Worlds ────────────────────────────────────────────────────────
            // The panel's world list is only ever a mirror of what is on disk;
            // these three commands are the only code paths that actually change
            // it. Every one of them re-validates the world name locally — the
            // API is not trusted to have done so.
            "set_active_world" => {
                let data_path = command.payload.get("data_path").and_then(|v| v.as_str()).unwrap_or("").to_string();
                let world = command.payload.get("world").and_then(|v| v.as_str()).unwrap_or("").to_string();

                if data_path.is_empty() || world.is_empty() {
                    CommandResult::err("set_active_world payload missing required fields".to_string())
                } else if !is_safe_world_name(&world) {
                    CommandResult::err(format!("set_active_world: unsafe world name {world}"))
                } else {
                    match write_active_world(&data_path, &world).await {
                        Ok(()) => {
                            info!(%server_id, %world, "Active world written to config.json");
                            CommandResult::ok(format!(
                                "Active world set to {world} in config.json — restart the server to load it"
                            ))
                        }
                        Err(err) => {
                            warn!(%err, %server_id, %world, "Failed to set active world");
                            CommandResult::err(err.to_string())
                        }
                    }
                }
            }

            "delete_world" => {
                let data_path = command.payload.get("data_path").and_then(|v| v.as_str()).unwrap_or("").to_string();
                let world = command.payload.get("world").and_then(|v| v.as_str()).unwrap_or("").to_string();

                if data_path.is_empty() || world.is_empty() {
                    CommandResult::err("delete_world payload missing required fields".to_string())
                } else if !is_safe_world_name(&world) {
                    CommandResult::err(format!("delete_world: unsafe world name {world}"))
                } else {
                    match delete_world_dir(&data_path, &world).await {
                        Ok(true) => {
                            info!(%server_id, %world, "World directory deleted");
                            CommandResult::ok(format!("Deleted world {world}"))
                        }
                        Ok(false) => {
                            // Already gone on disk — the DB row is authoritative
                            // for "removed", so this is a success, not an error.
                            info!(%server_id, %world, "World directory already absent");
                            CommandResult::ok(format!("World {world} was not present on disk"))
                        }
                        Err(err) => {
                            warn!(%err, %server_id, %world, "Failed to delete world");
                            CommandResult::err(err.to_string())
                        }
                    }
                }
            }

            "write_mod_config" => {
                let data_path = command.payload.get("data_path").and_then(|v| v.as_str()).unwrap_or("").to_string();
                let config = command.payload.get("config").cloned().unwrap_or(serde_json::Value::Null);

                if data_path.is_empty() || config.is_null() {
                    CommandResult::err("write_mod_config payload missing required fields".to_string())
                } else {
                    match write_mod_config_file(&data_path, &config).await {
                        Ok(()) => {
                            info!(%server_id, "Network mod config written");
                            CommandResult::ok(format!("Network config written for {server_id}"))
                        }
                        Err(err) => {
                            warn!(%err, %server_id, "Failed to write network mod config");
                            CommandResult::err(err.to_string())
                        }
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

/// Hard cap on a single mod download. The body is streamed to disk and the
/// download aborted past this, so a hostile or broken URL cannot exhaust the
/// memory or disk of a daemon shared by every server on the node.
const MAX_MOD_DOWNLOAD_BYTES: u64 = 512 * 1024 * 1024;

/// Redirects followed per download. Each hop is re-validated from scratch.
const MAX_MOD_REDIRECTS: usize = 5;

const MOD_CONNECT_TIMEOUT: std::time::Duration = std::time::Duration::from_secs(10);

/// Wall-clock budget for the whole download, redirects included.
const MOD_DOWNLOAD_TIMEOUT: std::time::Duration = std::time::Duration::from_secs(600);

/// Download a .jar file from `url` and save it to `{data_path}/mods/{filename}`.
///
/// The URL comes from any user holding mod.install, so the fetch is treated
/// as hostile: every hop (the URL and each redirect) must be https on port
/// 443 and resolve only to public addresses, and the connection is pinned to
/// the address that was checked so DNS cannot rebind between check and use.
/// The body is streamed into a temp file next to the target, capped at
/// `MAX_MOD_DOWNLOAD_BYTES`, and renamed into place only on success.
async fn download_and_install_mod(url: &str, data_path: &str, filename: &str) -> anyhow::Result<()> {
    if !is_safe_mod_filename(filename) {
        anyhow::bail!("unsafe filename {filename}");
    }
    tokio::fs::create_dir_all(data_path).await?;
    let mods_dir = std::path::Path::new(data_path).join("mods");
    ensure_real_dir(&mods_dir).await?;

    let target = mods_dir.join(filename);
    refuse_symlink(&target).await?;
    let tmp = mods_dir.join(format!(".{filename}.part"));
    let mut file = create_fresh_temp(&tmp).await?;

    let result = match tokio::time::timeout(MOD_DOWNLOAD_TIMEOUT, fetch_mod_into(url, &mut file)).await {
        Ok(r) => r,
        Err(_) => Err(anyhow::anyhow!("download timed out after {}s", MOD_DOWNLOAD_TIMEOUT.as_secs())),
    };
    let result = match result {
        Ok(()) => {
            drop(file);
            tokio::fs::rename(&tmp, &target).await.map_err(anyhow::Error::from)
        }
        Err(e) => {
            drop(file);
            Err(e)
        }
    };
    if result.is_err() {
        let _ = tokio::fs::remove_file(&tmp).await;
    }
    result
}

/// Fetch `raw_url`, following up to `MAX_MOD_REDIRECTS` redirects by hand so
/// each hop goes through `resolve_download_target`, and stream the final body
/// into `file`.
async fn fetch_mod_into(raw_url: &str, file: &mut tokio::fs::File) -> anyhow::Result<()> {
    use tokio::io::AsyncWriteExt;

    let mut url = reqwest::Url::parse(raw_url).map_err(|e| anyhow::anyhow!("invalid url: {e}"))?;
    let mut redirects = 0;
    let mut resp = loop {
        let target = resolve_download_target(&url).await.map_err(anyhow::Error::msg)?;
        let resp = pinned_download_client(&target)?.get(url.clone()).send().await?;
        if !resp.status().is_redirection() {
            break resp;
        }
        redirects += 1;
        if redirects > MAX_MOD_REDIRECTS {
            anyhow::bail!("too many redirects");
        }
        let location = resp
            .headers()
            .get(reqwest::header::LOCATION)
            .ok_or_else(|| anyhow::anyhow!("HTTP {} without a Location header", resp.status()))?
            .to_str()
            .map_err(|_| anyhow::anyhow!("redirect Location is not valid text"))?;
        url = url
            .join(location)
            .map_err(|e| anyhow::anyhow!("invalid redirect target: {e}"))?;
    };

    if !resp.status().is_success() {
        anyhow::bail!("Download failed: HTTP {}", resp.status());
    }
    if resp.content_length().is_some_and(|len| len > MAX_MOD_DOWNLOAD_BYTES) {
        anyhow::bail!("download exceeds the {} MiB limit", MAX_MOD_DOWNLOAD_BYTES / (1024 * 1024));
    }

    let mut written: u64 = 0;
    while let Some(chunk) = resp.chunk().await? {
        written += chunk.len() as u64;
        if written > MAX_MOD_DOWNLOAD_BYTES {
            anyhow::bail!("download exceeds the {} MiB limit", MAX_MOD_DOWNLOAD_BYTES / (1024 * 1024));
        }
        file.write_all(&chunk).await?;
    }
    file.flush().await?;
    file.sync_all().await?;
    Ok(())
}

/// A download hop that passed validation, and the address its connection must
/// use. `domain` is `None` for an IP-literal URL, which needs no pinning.
struct DownloadTarget {
    domain: Option<String>,
    addr: std::net::SocketAddr,
}

/// Validate one download hop: https on the default port, no credentials, and
/// a host whose every resolved address is public. Resolving here and pinning
/// the result into the client closes the DNS-rebinding window between the
/// check and the connection.
async fn resolve_download_target(url: &reqwest::Url) -> Result<DownloadTarget, String> {
    if url.scheme() != "https" {
        return Err("only https download URLs are allowed".to_string());
    }
    if !url.username().is_empty() || url.password().is_some() {
        return Err("download URLs must not carry credentials".to_string());
    }
    // Only the standard https port: a public IP that happens to be this node
    // must not become a way to reach whatever else it listens on.
    let port = url.port_or_known_default().unwrap_or(443);
    if port != 443 {
        return Err("download URLs must use the default https port".to_string());
    }
    let host = url.host_str().ok_or_else(|| "url has no host".to_string())?;

    // The url crate normalises IP literals (incl. octal/hex IPv4 forms), so a
    // literal here parses as an address and is checked without DNS.
    let literal = host
        .strip_prefix('[')
        .and_then(|h| h.strip_suffix(']'))
        .unwrap_or(host)
        .parse::<std::net::IpAddr>();
    if let Ok(ip) = literal {
        if !is_public_ip(ip) {
            return Err("download host is not a public address".to_string());
        }
        return Ok(DownloadTarget { domain: None, addr: std::net::SocketAddr::new(ip, port) });
    }

    let lower = host.trim_end_matches('.').to_ascii_lowercase();
    if lower == "localhost" || lower.ends_with(".localhost") || lower.ends_with(".internal") {
        return Err("download host not allowed".to_string());
    }
    let addrs: Vec<std::net::SocketAddr> = tokio::net::lookup_host((host, port))
        .await
        .map_err(|e| format!("could not resolve download host: {e}"))?
        .collect();
    let first = *addrs.first().ok_or_else(|| "download host did not resolve".to_string())?;
    // Refuse if *any* answer is internal: a mixed answer is a rebinding setup.
    if addrs.iter().any(|a| !is_public_ip(a.ip())) {
        return Err("download host resolves to a disallowed address".to_string());
    }
    Ok(DownloadTarget { domain: Some(host.to_string()), addr: first })
}

/// A client for exactly one request to `target`: redirects off (the caller
/// follows and re-validates them), proxies off (a proxy would resolve the host
/// itself and undo the pinning), TLS verification on.
fn pinned_download_client(target: &DownloadTarget) -> reqwest::Result<reqwest::Client> {
    let mut builder = reqwest::Client::builder()
        .redirect(reqwest::redirect::Policy::none())
        .https_only(true)
        .no_proxy()
        .connect_timeout(MOD_CONNECT_TIMEOUT)
        .timeout(MOD_DOWNLOAD_TIMEOUT);
    if let Some(domain) = &target.domain {
        builder = builder.resolve(domain, target.addr);
    }
    builder.build()
}

/// Fail if `path` is a symlink. Tenants can plant files in their server
/// directory, and the daemon must never write through a link they control.
async fn refuse_symlink(path: &std::path::Path) -> anyhow::Result<()> {
    match tokio::fs::symlink_metadata(path).await {
        Ok(md) if md.file_type().is_symlink() => {
            anyhow::bail!("refusing to write through symlink {}", path.display())
        }
        Ok(_) => Ok(()),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(()),
        Err(e) => Err(e.into()),
    }
}

/// Create `dir` if it is missing (its parent must exist) and require it to be
/// a real directory rather than a symlink to somewhere else on the node.
async fn ensure_real_dir(dir: &std::path::Path) -> anyhow::Result<()> {
    match tokio::fs::create_dir(dir).await {
        Ok(()) => {}
        Err(e) if e.kind() == std::io::ErrorKind::AlreadyExists => {}
        Err(e) => return Err(e.into()),
    }
    let md = tokio::fs::symlink_metadata(dir).await?;
    if md.file_type().is_symlink() || !md.is_dir() {
        anyhow::bail!("{} is not a real directory", dir.display());
    }
    Ok(())
}

/// Open `tmp` as a brand-new file. A stale temp from a crash — or a symlink
/// planted in its place — is unlinked first (remove_file removes a link, never
/// its target), and create_new (O_EXCL) refuses anything that reappears.
async fn create_fresh_temp(tmp: &std::path::Path) -> anyhow::Result<tokio::fs::File> {
    match tokio::fs::symlink_metadata(tmp).await {
        Ok(_) => tokio::fs::remove_file(tmp).await?,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => {}
        Err(e) => return Err(e.into()),
    }
    Ok(tokio::fs::OpenOptions::new().write(true).create_new(true).open(tmp).await?)
}

/// Directory a mod/plugin toggle defaults to when the payload omits "dir".
const DEFAULT_MOD_DIR: &str = "mods";

/// Directories a mod/plugin toggle is allowed to touch. Anything else is
/// rejected — the daemon never takes a caller-supplied directory verbatim.
const ALLOWED_MOD_DIRS: [&str; 2] = ["mods", "plugins"];

/// True if `data_path` is exactly `{servers_dir}/{server_id}` and `server_id`
/// is a canonical UUID: the only directory a command for that server may touch.
///
/// Compares lexically (a trailing slash is tolerated) because the directory
/// may not exist yet (install_mod creates it) and canonicalize would fail
/// there. Requiring the exact shape rules out the data root itself, other
/// servers' directories, siblings like backups/ and hytale-bin/, nested
/// paths, and any `..` or separator trickery.
fn is_server_data_path(data_path: &str, servers_dir: &str, server_id: &str) -> bool {
    let servers_dir = servers_dir.trim_end_matches('/');
    if servers_dir.is_empty() || data_path.contains('\0') || data_path.contains('\\') {
        return false;
    }
    let Ok(id) = uuid::Uuid::parse_str(server_id) else {
        return false;
    };
    // Only the canonical hyphenated form: parse_str also accepts braced, urn:
    // and simple forms, none of which the API ever produces.
    if server_id != id.hyphenated().to_string() {
        return false;
    }
    data_path.trim_end_matches('/') == format!("{servers_dir}/{server_id}")
}

/// Reject any path component that could escape its parent directory. Only a
/// bare basename is permitted — no path separators, parent refs, or NUL bytes.
fn is_safe_path_component(name: &str) -> bool {
    !name.is_empty()
        && name != "."
        && name != ".."
        && !name.contains('/')
        && !name.contains('\\')
        && !name.contains("..")
        && !name.contains('\0')
}

/// Reject mod filenames that could escape the mods directory, so
/// `{data_path}/{dir}/{filename}` cannot be steered elsewhere on the node.
fn is_safe_mod_filename(name: &str) -> bool {
    is_safe_path_component(name)
}

/// Reject world names that could escape `universe/worlds/`. This guards a
/// recursive directory delete — treat every relaxation here as a CVE.
fn is_safe_world_name(name: &str) -> bool {
    is_safe_path_component(name)
}

/// True if `dir` is a directory mods/plugins may live in.
fn is_allowed_mod_dir(dir: &str) -> bool {
    ALLOWED_MOD_DIRS.contains(&dir)
}

/// Write `world` into `Defaults.World` of `{data_path}/config.json`, preserving
/// every other key. The Hytale server only reads this file at boot, so the
/// caller must tell the operator that a restart is required.
async fn write_active_world(data_path: &str, world: &str) -> anyhow::Result<()> {
    if !is_safe_world_name(world) {
        anyhow::bail!("unsafe world name: {world}");
    }

    let cfg_path = std::path::Path::new(data_path).join("config.json");
    // A tenant-planted config.json -> /etc/... link must be neither read nor
    // replaced.
    refuse_symlink(&cfg_path).await?;

    // Parse the existing config if there is one; otherwise start from an empty
    // object so a not-yet-started server can still be pointed at a world.
    let mut cfg: serde_json::Value = match tokio::fs::read_to_string(&cfg_path).await {
        Ok(contents) => serde_json::from_str(&contents)
            .map_err(|e| anyhow::anyhow!("config.json is not valid JSON: {e}"))?,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => serde_json::json!({}),
        Err(e) => return Err(anyhow::anyhow!("could not read config.json: {e}")),
    };

    if !cfg.is_object() {
        anyhow::bail!("config.json does not contain a JSON object");
    }
    let defaults = cfg
        .as_object_mut()
        .expect("checked above")
        .entry("Defaults")
        .or_insert_with(|| serde_json::json!({}));
    if !defaults.is_object() {
        anyhow::bail!("config.json: Defaults is not a JSON object");
    }
    defaults
        .as_object_mut()
        .expect("checked above")
        .insert("World".to_string(), serde_json::Value::String(world.to_string()));

    // Write via a temp file + rename so a crash mid-write cannot leave the
    // server with a truncated config.json.
    let tmp_path = cfg_path.with_extension("json.tmp");
    let body = serde_json::to_string_pretty(&cfg)?;
    write_file_atomic(&cfg_path, &tmp_path, body.as_bytes()).await
}

/// Replace `target` with `body` via `tmp` + rename, without following any
/// symlink a tenant may have planted at either path: the target must not be a
/// link, and the temp is always a freshly created regular file.
async fn write_file_atomic(target: &std::path::Path, tmp: &std::path::Path, body: &[u8]) -> anyhow::Result<()> {
    use tokio::io::AsyncWriteExt;

    refuse_symlink(target).await?;
    let mut file = create_fresh_temp(tmp).await?;
    let result: anyhow::Result<()> = async {
        file.write_all(body).await?;
        file.flush().await?;
        file.sync_all().await?;
        Ok(())
    }
    .await;
    drop(file);
    let result = match result {
        Ok(()) => tokio::fs::rename(tmp, target).await.map_err(anyhow::Error::from),
        Err(e) => Err(e),
    };
    if result.is_err() {
        let _ = tokio::fs::remove_file(tmp).await;
    }
    result
}

/// Recursively delete `{data_path}/universe/worlds/{world}`.
///
/// Returns `Ok(false)` when the directory does not exist (already gone), so the
/// caller can report success instead of a spurious failure.
async fn delete_world_dir(data_path: &str, world: &str) -> anyhow::Result<bool> {
    if !is_safe_world_name(world) {
        anyhow::bail!("unsafe world name: {world}");
    }

    let worlds_root = std::path::Path::new(data_path).join("universe").join("worlds");
    let target = worlds_root.join(world);

    // Belt and braces: the name is already a validated single component, but
    // re-assert that the join stayed inside the worlds root before deleting.
    if !target.starts_with(&worlds_root) {
        anyhow::bail!("refusing to delete outside universe/worlds: {}", target.display());
    }

    match tokio::fs::metadata(&target).await {
        Ok(md) if md.is_dir() => {
            tokio::fs::remove_dir_all(&target).await?;
            Ok(true)
        }
        Ok(_) => anyhow::bail!("{} is not a directory", target.display()),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => Ok(false),
        Err(e) => Err(anyhow::anyhow!("could not stat world directory: {e}")),
    }
}

/// Write the TalePanel network mod config to {data_path}/talepanel/network.json,
/// creating the directory if needed. It lives outside mods/ because Hytale
/// treats every folder there as an asset pack ("Skipping pack ... missing
/// manifest.json"). Atomic via temp file + rename so a crash mid-write cannot
/// leave a half-written config the mod would fail to parse. The path is fixed
/// (no caller-supplied filename); data_path was already checked to be this
/// server's directory at the top of the poll loop.
async fn write_mod_config_file(data_path: &str, config: &serde_json::Value) -> anyhow::Result<()> {
    let root = std::path::Path::new(data_path);
    tokio::fs::create_dir_all(root).await?;
    let dir = root.join("talepanel");
    ensure_real_dir(&dir).await?;

    let cfg_path = dir.join("network.json");
    let tmp_path = dir.join("network.json.tmp");
    let body = serde_json::to_string_pretty(config)?;
    write_file_atomic(&cfg_path, &tmp_path, body.as_bytes()).await?;

    // Remove the config older daemons wrote into mods/talepanel-network/.
    // Only our own files are deleted, and the dir only if that left it empty.
    // Both path components are tenant-writable, so only descend into them if
    // they are real directories — never delete through a planted symlink.
    let mods = root.join("mods");
    let legacy = mods.join("talepanel-network");
    if is_real_dir(&mods).await && is_real_dir(&legacy).await {
        for name in ["config.json", "config.json.tmp"] {
            let _ = tokio::fs::remove_file(legacy.join(name)).await;
        }
        let _ = tokio::fs::remove_dir(&legacy).await;
    }
    Ok(())
}

/// True if `path` exists and is a directory, not a symlink to one.
async fn is_real_dir(path: &std::path::Path) -> bool {
    tokio::fs::symlink_metadata(path)
        .await
        .is_ok_and(|md| md.file_type().is_dir())
}

/// True only for globally routable unicast addresses a mod download may reach.
///
/// Everything else is refused: loopback, RFC 1918, CGNAT (100.64.0.0/10, which
/// also holds Alibaba's 100.100.100.200 metadata endpoint), link-local (incl.
/// 169.254.169.254), benchmarking, documentation, IETF-reserved, multicast,
/// class E and broadcast. An IPv4-mapped IPv6 address is judged by the IPv4
/// address it carries; the other IPv4-embedding forms (v4-compatible, NAT64,
/// 6to4, Teredo) are refused outright. IPv6 is an allowlist: only 2000::/3
/// global unicast minus its special-purpose blocks passes, so ULA, link-local
/// and multicast cannot.
fn is_public_ip(ip: std::net::IpAddr) -> bool {
    match ip {
        std::net::IpAddr::V4(v4) => is_public_ipv4(v4),
        std::net::IpAddr::V6(v6) => is_public_ipv6(v6),
    }
}

fn is_public_ipv4(ip: std::net::Ipv4Addr) -> bool {
    let [a, b, c, _] = ip.octets();
    let blocked = a == 0                                // 0.0.0.0/8 "this network"
        || a == 10                                      // 10.0.0.0/8 private
        || (a == 100 && (b & 0xC0) == 64)               // 100.64.0.0/10 CGNAT
        || a == 127                                     // 127.0.0.0/8 loopback
        || (a == 169 && b == 254)                       // 169.254.0.0/16 link-local
        || (a == 172 && (b & 0xF0) == 16)               // 172.16.0.0/12 private
        || (a == 192 && b == 0 && c == 0)               // 192.0.0.0/24 IETF protocol
        || (a == 192 && b == 0 && c == 2)               // 192.0.2.0/24 TEST-NET-1
        || (a == 192 && b == 88 && c == 99)             // 192.88.99.0/24 6to4 relay
        || (a == 192 && b == 168)                       // 192.168.0.0/16 private
        || (a == 198 && (b & 0xFE) == 18)               // 198.18.0.0/15 benchmarking
        || (a == 198 && b == 51 && c == 100)            // 198.51.100.0/24 TEST-NET-2
        || (a == 203 && b == 0 && c == 113)             // 203.0.113.0/24 TEST-NET-3
        || a >= 224;                                    // multicast, 240/4, broadcast
    !blocked
}

fn is_public_ipv6(ip: std::net::Ipv6Addr) -> bool {
    // ::ffff:a.b.c.d reaches the IPv4 address on a dual-stack socket, so it is
    // exactly as public as that address.
    if let Some(v4) = ip.to_ipv4_mapped() {
        return is_public_ipv4(v4);
    }
    let s = ip.segments();
    // Allowlist: global unicast is 2000::/3. This alone refuses ::, ::1, the
    // deprecated IPv4-compatible ::a.b.c.d, NAT64 64:ff9b::/96 and
    // 64:ff9b:1::/48, discard-only 100::/64, ULA fc00::/7, link-local
    // fe80::/10, site-local fec0::/10 and multicast ff00::/8.
    if (s[0] & 0xE000) != 0x2000 {
        return false;
    }
    let blocked = (s[0] == 0x2001 && s[1] < 0x0200)     // 2001::/23 IETF protocol (Teredo, ORCHID, benchmarking)
        || (s[0] == 0x2001 && s[1] == 0x0db8)           // 2001:db8::/32 documentation
        || s[0] == 0x2002                               // 2002::/16 6to4, embeds an arbitrary IPv4
        || (s[0] & 0xFFF0) == 0x3FF0;                   // 3fff::/20 documentation
    !blocked
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::path::{Path, PathBuf};
    use std::sync::atomic::{AtomicU32, Ordering};

    /// delete_world hands data_path to remove_dir_all. If the API is ever
    /// compromised, this check is what stops `data_path = "/"`, or another
    /// tenant's server directory.
    #[test]
    fn data_path_must_be_exactly_this_servers_directory() {
        let dir = "/srv/taledaemon/servers";
        let id = "0b7c5a3e-9f1d-4c2a-8e6b-1d2f3a4b5c6d";
        let own = format!("{dir}/{id}");

        assert!(is_server_data_path(&own, dir, id));
        assert!(is_server_data_path(&format!("{own}/"), dir, id));
        assert!(is_server_data_path(&own, "/srv/taledaemon/servers/", id));

        for bad in [
            "",
            "/",
            "/etc",
            "/srv/taledaemon",
            "/srv/taledaemon/servers",
            "/srv/taledaemon/servers/",
            "/srv/taledaemon/backups",
            "/srv/taledaemon/hytale-bin",
            "/srv/taledaemon/servers-evil/0b7c5a3e-9f1d-4c2a-8e6b-1d2f3a4b5c6d",
            "/srv/taledaemon/servers/11111111-2222-4333-8444-555555555555",
            "/srv/taledaemon/servers/0b7c5a3e-9f1d-4c2a-8e6b-1d2f3a4b5c6d/mods",
            "/srv/taledaemon/servers/0b7c5a3e-9f1d-4c2a-8e6b-1d2f3a4b5c6d/..",
            "/srv/taledaemon/servers/../servers/0b7c5a3e-9f1d-4c2a-8e6b-1d2f3a4b5c6d",
            "/srv/taledaemon/servers//0b7c5a3e-9f1d-4c2a-8e6b-1d2f3a4b5c6d",
            "/srv/taledaemon/servers\\0b7c5a3e-9f1d-4c2a-8e6b-1d2f3a4b5c6d",
            "/srv/taledaemon/servers/0b7c5a3e-9f1d-4c2a-8e6b-1d2f3a4b5c6d\0",
            "srv/taledaemon/servers/0b7c5a3e-9f1d-4c2a-8e6b-1d2f3a4b5c6d",
        ] {
            assert!(!is_server_data_path(bad, dir, id), "{bad:?} must be rejected");
        }

        // The id must be a canonical UUID, not something path-shaped.
        for bad_id in [
            "",
            "..",
            "abc",
            "../x",
            "0B7C5A3E-9F1D-4C2A-8E6B-1D2F3A4B5C6D",
            "0b7c5a3e9f1d4c2a8e6b1d2f3a4b5c6d",
        ] {
            assert!(!is_server_data_path(&format!("{dir}/{bad_id}"), dir, bad_id), "{bad_id:?}");
        }
        assert!(!is_server_data_path(&own, "", id));
        assert!(!is_server_data_path(&format!("/{id}"), "/", id));
    }

    // ── SSRF address filter ───────────────────────────────────────────────────

    #[test]
    fn public_ip_allows_global_unicast() {
        for addr in [
            "1.1.1.1",
            "8.8.8.8",
            "104.16.0.1",
            "100.63.255.255",   // just below CGNAT
            "100.128.0.0",      // just above CGNAT
            "172.15.255.255",   // just below 172.16/12
            "172.32.0.0",       // just above 172.16/12
            "198.17.255.255",   // just below benchmarking
            "198.20.0.0",       // just above benchmarking
            "223.255.255.255",  // last unicast before multicast
            "2606:4700:4700::1111",
            "2a00:1450:4001::200e",
            "2001:4860:4860::8888",
            "2001:200::1",      // first address after 2001::/23
            "::ffff:1.1.1.1",   // v4-mapped public
        ] {
            let ip: std::net::IpAddr = addr.parse().unwrap();
            assert!(is_public_ip(ip), "{addr} should be public");
        }
    }

    #[test]
    fn public_ip_blocks_internal_and_special_ranges() {
        for addr in [
            // IPv4
            "0.0.0.0",
            "0.1.2.3",
            "10.0.0.1",
            "100.64.0.1",
            "100.100.100.200",  // Alibaba metadata
            "100.127.255.255",
            "127.0.0.1",
            "127.255.255.254",
            "169.254.169.254",  // AWS/GCP/Azure metadata
            "172.16.0.1",
            "172.31.255.255",
            "192.0.0.1",
            "192.0.0.170",
            "192.0.2.1",
            "192.88.99.1",
            "192.168.1.1",
            "198.18.0.1",
            "198.19.255.255",
            "198.51.100.1",
            "203.0.113.1",
            "224.0.0.1",
            "239.255.255.250",
            "240.0.0.1",
            "255.255.255.255",
            // IPv6
            "::",
            "::1",
            "::ffff:10.0.0.1",          // v4-mapped private
            "::ffff:127.0.0.1",
            "::ffff:169.254.169.254",
            "::ffff:100.100.100.200",
            "::10.0.0.1",               // v4-compatible
            "::8.8.8.8",                // v4-compatible, refused regardless
            "64:ff9b::a00:1",           // NAT64 -> 10.0.0.1
            "64:ff9b::808:808",         // NAT64, refused regardless
            "64:ff9b:1::1",             // local-use NAT64
            "100::1",                   // discard-only
            "fc00::1",
            "fd12:3456:789a::1",        // ULA
            "fe80::1",                  // link-local
            "febf::1",
            "fec0::1",                  // site-local (deprecated)
            "ff02::1",                  // multicast
            "ff0e::1",
            "2001::1",                  // Teredo
            "2001:2::1",                // benchmarking
            "2001:10::1",               // ORCHID
            "2001:db8::1",              // documentation
            "2002:a00:1::1",            // 6to4 -> 10.0.0.1
            "3fff::1",                  // documentation (RFC 9637)
            "4000::1",                  // outside 2000::/3
        ] {
            let ip: std::net::IpAddr = addr.parse().unwrap();
            assert!(!is_public_ip(ip), "{addr} must be blocked");
        }
    }

    async fn target_err(raw: &str) -> String {
        let url = reqwest::Url::parse(raw).unwrap();
        match resolve_download_target(&url).await {
            Ok(_) => panic!("{raw} must be rejected"),
            Err(e) => e,
        }
    }

    #[tokio::test]
    async fn download_target_rejects_unsafe_urls_without_dns() {
        assert!(target_err("http://1.1.1.1/m.jar").await.contains("https"));
        assert!(target_err("https://1.1.1.1:8443/m.jar").await.contains("port"));
        assert!(target_err("https://u:p@1.1.1.1/m.jar").await.contains("credentials"));
        assert!(target_err("https://localhost/m.jar").await.contains("not allowed"));
        assert!(target_err("https://metadata.google.internal/m.jar").await.contains("not allowed"));
        for raw in [
            "https://169.254.169.254/latest/meta-data/",
            "https://100.100.100.200/",
            "https://127.0.0.1/m.jar",
            "https://0x7f.1/m.jar",          // url crate normalises to 127.0.0.1
            "https://2130706433/m.jar",      // ditto, decimal form
            "https://[::1]/m.jar",
            "https://[::ffff:10.0.0.1]/m.jar",
            "https://[fd00::1]/m.jar",
            "https://[fe80::1]/m.jar",
        ] {
            assert!(target_err(raw).await.contains("not a public address"), "{raw}");
        }
    }

    #[tokio::test]
    async fn download_target_accepts_public_ip_literal_unpinned() {
        let url = reqwest::Url::parse("https://1.1.1.1/m.jar").unwrap();
        let t = resolve_download_target(&url).await.expect("public literal is fine");
        assert!(t.domain.is_none());
        assert_eq!(t.addr, "1.1.1.1:443".parse().unwrap());

        let url = reqwest::Url::parse("https://[2606:4700:4700::1111]/m.jar").unwrap();
        let t = resolve_download_target(&url).await.expect("public v6 literal is fine");
        assert_eq!(t.addr.ip(), "2606:4700:4700::1111".parse::<std::net::IpAddr>().unwrap());
    }

    #[tokio::test]
    async fn failed_mod_download_leaves_nothing_behind() {
        let root = scratch_dir("mod-ssrf");
        let data_path = root.join("server");

        let err = download_and_install_mod("https://169.254.169.254/latest", data_path.to_str().unwrap(), "x.jar")
            .await
            .expect_err("internal target must be refused");
        assert!(err.to_string().contains("not a public address"), "{err}");

        let mods = data_path.join("mods");
        assert!(!mods.join("x.jar").exists());
        assert!(!mods.join(".x.jar.part").exists(), "partial download must be removed");

        let _ = std::fs::remove_dir_all(&root);
    }

    #[cfg(unix)]
    #[tokio::test]
    async fn mod_install_refuses_symlinked_mods_dir() {
        let root = scratch_dir("mod-symlink-dir");
        let data_path = root.join("server");
        let elsewhere = root.join("elsewhere");
        std::fs::create_dir_all(&data_path).unwrap();
        std::fs::create_dir_all(&elsewhere).unwrap();
        std::os::unix::fs::symlink(&elsewhere, data_path.join("mods")).unwrap();

        let err = download_and_install_mod("https://1.1.1.1/x.jar", data_path.to_str().unwrap(), "x.jar")
            .await
            .expect_err("symlinked mods/ must be refused");
        assert!(err.to_string().contains("not a real directory"), "{err}");
        assert_eq!(std::fs::read_dir(&elsewhere).unwrap().count(), 0, "nothing written through the link");

        let _ = std::fs::remove_dir_all(&root);
    }

    #[cfg(unix)]
    #[tokio::test]
    async fn fresh_temp_unlinks_a_planted_symlink_instead_of_following_it() {
        let root = scratch_dir("fresh-temp");
        let victim = root.join("victim");
        std::fs::write(&victim, b"keep").unwrap();
        let tmp = root.join("config.json.tmp");
        std::os::unix::fs::symlink(&victim, &tmp).unwrap();

        let mut f = create_fresh_temp(&tmp).await.expect("temp must be created");
        tokio::io::AsyncWriteExt::write_all(&mut f, b"new").await.unwrap();
        tokio::io::AsyncWriteExt::flush(&mut f).await.unwrap();
        drop(f);

        assert_eq!(std::fs::read(&victim).unwrap(), b"keep", "link target must be untouched");
        assert!(!std::fs::symlink_metadata(&tmp).unwrap().file_type().is_symlink());
        assert_eq!(std::fs::read(&tmp).unwrap(), b"new");

        let _ = std::fs::remove_dir_all(&root);
    }

    #[cfg(unix)]
    #[tokio::test]
    async fn write_active_world_refuses_symlinked_config() {
        let root = scratch_dir("active-world-symlink");
        let data_path = root.join("server");
        std::fs::create_dir_all(&data_path).unwrap();
        let victim = root.join("victim.json");
        std::fs::write(&victim, br#"{"secret":true}"#).unwrap();
        std::os::unix::fs::symlink(&victim, data_path.join("config.json")).unwrap();

        let err = write_active_world(data_path.to_str().unwrap(), "w")
            .await
            .expect_err("symlinked config.json must be refused");
        assert!(err.to_string().contains("symlink"), "{err}");
        assert_eq!(std::fs::read(&victim).unwrap(), br#"{"secret":true}"#);

        let _ = std::fs::remove_dir_all(&root);
    }

    #[cfg(unix)]
    #[tokio::test]
    async fn write_active_world_does_not_follow_a_planted_temp_symlink() {
        let root = scratch_dir("active-world-tmp-symlink");
        let data_path = root.join("server");
        std::fs::create_dir_all(&data_path).unwrap();
        let victim = root.join("victim");
        std::fs::write(&victim, b"keep").unwrap();
        std::os::unix::fs::symlink(&victim, data_path.join("config.json.tmp")).unwrap();

        write_active_world(data_path.to_str().unwrap(), "w")
            .await
            .expect("write should succeed");

        assert_eq!(std::fs::read(&victim).unwrap(), b"keep", "link target must be untouched");
        let cfg = data_path.join("config.json");
        assert!(!std::fs::symlink_metadata(&cfg).unwrap().file_type().is_symlink());
        let parsed: serde_json::Value = serde_json::from_str(&std::fs::read_to_string(&cfg).unwrap()).unwrap();
        assert_eq!(parsed["Defaults"]["World"], "w");

        let _ = std::fs::remove_dir_all(&root);
    }

    #[cfg(unix)]
    #[tokio::test]
    async fn write_mod_config_file_refuses_symlinks() {
        let root = scratch_dir("modconfig-symlink");
        let data_path = root.join("server");
        let elsewhere = root.join("elsewhere");
        std::fs::create_dir_all(&data_path).unwrap();
        std::fs::create_dir_all(&elsewhere).unwrap();
        let config = serde_json::json!({ "role": "hub" });

        // talepanel/ pointing elsewhere.
        std::os::unix::fs::symlink(&elsewhere, data_path.join("talepanel")).unwrap();
        let err = write_mod_config_file(data_path.to_str().unwrap(), &config)
            .await
            .expect_err("symlinked talepanel/ must be refused");
        assert!(err.to_string().contains("not a real directory"), "{err}");
        assert_eq!(std::fs::read_dir(&elsewhere).unwrap().count(), 0);
        std::fs::remove_file(data_path.join("talepanel")).unwrap();

        // network.json pointing elsewhere.
        let victim = elsewhere.join("victim.json");
        std::fs::write(&victim, b"keep").unwrap();
        std::fs::create_dir_all(data_path.join("talepanel")).unwrap();
        std::os::unix::fs::symlink(&victim, data_path.join("talepanel").join("network.json")).unwrap();
        let err = write_mod_config_file(data_path.to_str().unwrap(), &config)
            .await
            .expect_err("symlinked network.json must be refused");
        assert!(err.to_string().contains("symlink"), "{err}");
        assert_eq!(std::fs::read(&victim).unwrap(), b"keep");
        std::fs::remove_file(data_path.join("talepanel").join("network.json")).unwrap();

        // Legacy cleanup must not delete through mods/talepanel-network -> elsewhere.
        std::fs::write(elsewhere.join("config.json"), b"keep").unwrap();
        std::fs::create_dir_all(data_path.join("mods")).unwrap();
        std::os::unix::fs::symlink(&elsewhere, data_path.join("mods").join("talepanel-network")).unwrap();
        write_mod_config_file(data_path.to_str().unwrap(), &config)
            .await
            .expect("write should succeed");
        assert!(elsewhere.join("config.json").exists(), "cleanup must not follow the link");

        let _ = std::fs::remove_dir_all(&root);
    }

    static COUNTER: AtomicU32 = AtomicU32::new(0);

    /// Create a unique, empty scratch directory under the OS temp dir.
    fn scratch_dir(tag: &str) -> PathBuf {
        let n = COUNTER.fetch_add(1, Ordering::SeqCst);
        let dir = std::env::temp_dir().join(format!("taledaemon-test-{tag}-{}-{n}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).expect("create scratch dir");
        dir
    }

    fn make_world(data_path: &Path, name: &str) -> PathBuf {
        let dir = data_path.join("universe").join("worlds").join(name);
        std::fs::create_dir_all(&dir).expect("create world dir");
        std::fs::write(dir.join("level.dat"), b"chunks").expect("write world file");
        dir
    }

    // ── Path-traversal guard ──────────────────────────────────────────────────

    #[test]
    fn safe_world_name_accepts_plain_names() {
        for name in ["overworld", "my-world", "world_2", "Welt 1", "world.backup"] {
            assert!(is_safe_world_name(name), "{name} should be accepted");
        }
    }

    #[test]
    fn safe_world_name_rejects_traversal_and_separators() {
        for name in [
            "",
            ".",
            "..",
            "../etc",
            "../../srv",
            "worlds/../../..",
            "a/b",
            "a\\b",
            "/etc/passwd",
            "C:\\Windows",
            "world\0evil",
        ] {
            assert!(!is_safe_world_name(name), "{name:?} must be rejected");
        }
    }

    #[test]
    fn allowed_mod_dirs_are_exactly_mods_and_plugins() {
        assert!(is_allowed_mod_dir("mods"));
        assert!(is_allowed_mod_dir("plugins"));
        for dir in ["", "..", "../mods", "config", "universe", "/etc"] {
            assert!(!is_allowed_mod_dir(dir), "{dir:?} must be rejected");
        }
        assert_eq!(DEFAULT_MOD_DIR, "mods", "default must stay backwards compatible");
    }

    // ── delete_world ──────────────────────────────────────────────────────────

    #[tokio::test]
    async fn delete_world_dir_removes_only_the_named_world() {
        let root = scratch_dir("delete-ok");
        let data_path = root.join("server");
        let keep = make_world(&data_path, "keepme");
        let doomed = make_world(&data_path, "doomed");

        let removed = delete_world_dir(data_path.to_str().unwrap(), "doomed")
            .await
            .expect("delete should succeed");

        assert!(removed);
        assert!(!doomed.exists(), "target world must be gone");
        assert!(keep.exists(), "sibling world must survive");

        let _ = std::fs::remove_dir_all(&root);
    }

    #[tokio::test]
    async fn delete_world_dir_refuses_traversal_and_deletes_nothing() {
        let root = scratch_dir("delete-traversal");
        let data_path = root.join("server");
        // A file the escape attempt would reach if the guard were missing:
        // {data_path}/universe/worlds/../../../secret  ==  {root}/../secret
        make_world(&data_path, "overworld");
        let secret = root.join("secret");
        std::fs::create_dir_all(&secret).expect("create secret dir");
        std::fs::write(secret.join("keys.txt"), b"top secret").expect("write secret");

        for evil in ["../../../secret", "..", "../worlds", "a/../../secret"] {
            let err = delete_world_dir(data_path.to_str().unwrap(), evil)
                .await
                .expect_err("traversal must be rejected");
            assert!(
                err.to_string().contains("unsafe world name"),
                "unexpected error for {evil:?}: {err}"
            );
        }

        assert!(secret.join("keys.txt").exists(), "guard must not delete outside universe/worlds");
        assert!(data_path.join("universe").join("worlds").join("overworld").exists());

        let _ = std::fs::remove_dir_all(&root);
    }

    #[tokio::test]
    async fn delete_world_dir_is_idempotent_when_world_is_absent() {
        let root = scratch_dir("delete-absent");
        let data_path = root.join("server");
        std::fs::create_dir_all(data_path.join("universe").join("worlds")).unwrap();

        let removed = delete_world_dir(data_path.to_str().unwrap(), "ghost")
            .await
            .expect("absent world must not be an error");
        assert!(!removed);

        let _ = std::fs::remove_dir_all(&root);
    }

    // ── set_active_world ──────────────────────────────────────────────────────

    #[tokio::test]
    async fn write_active_world_sets_defaults_world_and_preserves_other_keys() {
        let root = scratch_dir("active-world");
        let data_path = root.join("server");
        std::fs::create_dir_all(&data_path).unwrap();
        std::fs::write(
            data_path.join("config.json"),
            r#"{"Defaults":{"World":"old","GameMode":"survival"},"Network":{"Port":25565}}"#,
        )
        .unwrap();

        write_active_world(data_path.to_str().unwrap(), "newworld")
            .await
            .expect("write should succeed");

        let raw = std::fs::read_to_string(data_path.join("config.json")).unwrap();
        let cfg: serde_json::Value = serde_json::from_str(&raw).unwrap();
        assert_eq!(cfg["Defaults"]["World"], "newworld");
        assert_eq!(cfg["Defaults"]["GameMode"], "survival", "unrelated keys must survive");
        assert_eq!(cfg["Network"]["Port"], 25565, "unrelated sections must survive");
        assert!(!data_path.join("config.json.tmp").exists(), "temp file must be renamed away");

        let _ = std::fs::remove_dir_all(&root);
    }

    #[tokio::test]
    async fn write_active_world_creates_config_when_missing() {
        let root = scratch_dir("active-world-new");
        let data_path = root.join("server");
        std::fs::create_dir_all(&data_path).unwrap();

        write_active_world(data_path.to_str().unwrap(), "fresh")
            .await
            .expect("write should succeed");

        let raw = std::fs::read_to_string(data_path.join("config.json")).unwrap();
        let cfg: serde_json::Value = serde_json::from_str(&raw).unwrap();
        assert_eq!(cfg["Defaults"]["World"], "fresh");

        let _ = std::fs::remove_dir_all(&root);
    }

    #[tokio::test]
    async fn write_active_world_rejects_traversal() {
        let root = scratch_dir("active-world-evil");
        let data_path = root.join("server");
        std::fs::create_dir_all(&data_path).unwrap();

        let err = write_active_world(data_path.to_str().unwrap(), "../../etc/passwd")
            .await
            .expect_err("traversal must be rejected");
        assert!(err.to_string().contains("unsafe world name"));
        assert!(!data_path.join("config.json").exists(), "nothing must be written");

        let _ = std::fs::remove_dir_all(&root);
    }

    #[tokio::test]
    async fn write_mod_config_file_writes_atomically_outside_mods() {
        let root = scratch_dir("modconfig");
        let data_path = root.join("server");
        std::fs::create_dir_all(&data_path).unwrap();
        // A config left by older daemons inside mods/ — Hytale treats every
        // folder in mods/ as an asset pack and warns about it.
        let legacy_dir = data_path.join("mods").join("talepanel-network");
        std::fs::create_dir_all(&legacy_dir).unwrap();
        std::fs::write(legacy_dir.join("config.json"), "{}").unwrap();

        let config = serde_json::json!({
            "version": 1,
            "role": "hub",
            "secrets": ["deadbeef"],
        });

        write_mod_config_file(data_path.to_str().unwrap(), &config)
            .await
            .expect("write should succeed");

        let cfg_path = data_path.join("talepanel").join("network.json");
        assert!(cfg_path.exists(), "network.json must exist");
        let raw = std::fs::read_to_string(&cfg_path).unwrap();
        let parsed: serde_json::Value = serde_json::from_str(&raw).unwrap();
        assert_eq!(parsed["role"], "hub");
        assert_eq!(parsed["secrets"][0], "deadbeef");
        assert!(
            !data_path.join("talepanel").join("network.json.tmp").exists(),
            "temp file must be renamed away"
        );
        assert!(!legacy_dir.exists(), "the legacy config dir in mods/ must be removed");

        let _ = std::fs::remove_dir_all(&root);
    }
}
