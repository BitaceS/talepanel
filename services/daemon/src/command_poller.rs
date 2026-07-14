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
                poll_and_dispatch(&api_client, &process_manager, &config.daemon.data_root).await;
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
    data_root: &str,
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
        // where the next mistake would land.
        if let Some(dp) = command.payload.get("data_path").and_then(|v| v.as_str()) {
            if !is_within_data_root(dp, data_root) {
                warn!(%server_id, %command_type, data_path = %dp,
                    "Rejected command: data_path is outside the daemon data root");
                let result = CommandResult::err(format!(
                    "{command_type}: data_path {dp} is outside the daemon data root"
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

/// Directory a mod/plugin toggle defaults to when the payload omits "dir".
const DEFAULT_MOD_DIR: &str = "mods";

/// Directories a mod/plugin toggle is allowed to touch. Anything else is
/// rejected — the daemon never takes a caller-supplied directory verbatim.
const ALLOWED_MOD_DIRS: [&str; 2] = ["mods", "plugins"];

/// Reject any path component that could escape its parent directory. Only a
/// bare basename is permitted — no path separators, parent refs, or NUL bytes.
/// True if `data_path` resolves to somewhere inside the daemon's data root.
///
/// Compares lexically after normalising separators and rejecting `..`, because
/// the directory may not exist yet (install_mod creates it) and canonicalize
/// would fail there.  A `..`-free absolute path under the root cannot escape it.
fn is_within_data_root(data_path: &str, data_root: &str) -> bool {
    if data_path.is_empty() || data_root.is_empty() {
        return false;
    }
    let norm = |s: &str| s.replace('\\', "/").trim_end_matches('/').to_string();
    let path = norm(data_path);
    let root = norm(data_root);

    if path.split('/').any(|c| c == "..") || path.contains('\0') {
        return false;
    }
    path == root || path.starts_with(&format!("{root}/"))
}

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
    tokio::fs::write(&tmp_path, body).await?;
    tokio::fs::rename(&tmp_path, &cfg_path).await?;
    Ok(())
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

#[cfg(test)]
mod tests {
    use super::*;
    use std::path::{Path, PathBuf};
    use std::sync::atomic::{AtomicU32, Ordering};

    /// delete_world hands data_path to remove_dir_all. If the API is ever
    /// compromised, this check is what stops `data_path = "/"`.
    #[test]
    fn data_path_must_stay_inside_the_data_root() {
        let root = "/srv/taledaemon/servers";

        assert!(is_within_data_root("/srv/taledaemon/servers/abc-123", root));
        assert!(is_within_data_root("/srv/taledaemon/servers", root));
        assert!(is_within_data_root("/srv/taledaemon/servers/abc/", root));

        assert!(!is_within_data_root("/", root));
        assert!(!is_within_data_root("/etc", root));
        assert!(!is_within_data_root("/srv/taledaemon/servers/../../..", root));
        assert!(!is_within_data_root("/srv/taledaemon/servers-evil", root));
        assert!(!is_within_data_root("", root));
        assert!(!is_within_data_root("/srv/taledaemon/servers/a\0b", root));
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
}
