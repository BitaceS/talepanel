use std::path::Path;
use std::sync::Arc;

use anyhow::Result;
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use tokio::sync::broadcast;
use tracing::{debug, info, warn};

use crate::api_client::ApiClient;
use crate::config::Config;

/// Detected plugin metadata reported to the panel API.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DetectedPlugin {
    /// Base filename *without* a trailing `.disabled` suffix, so the panel key
    /// stays stable across enable/disable toggles.
    pub filename: String,
    pub plugin_name: String,
    pub version: String,
    pub author: String,
    pub description: String,
    pub commands: Vec<String>,
    pub config_files: Vec<String>,
    pub file_hash: String,
    /// False when the file on disk carries the `.disabled` suffix — the same
    /// rename-based convention `enable_mod`/`disable_mod` use.
    #[serde(default = "default_true")]
    pub enabled: bool,
    /// Directory the file was found in: "mods" or "plugins". The panel echoes
    /// this back in the toggle command so the daemon renames the right file.
    #[serde(default = "default_source_dir")]
    pub source_dir: String,
}

fn default_true() -> bool {
    true
}

fn default_source_dir() -> String {
    "mods".to_string()
}

/// Metadata extracted from plugin.yml (Bukkit/Spigot/Paper style).
#[derive(Debug, Deserialize, Default)]
struct PluginYml {
    name: Option<String>,
    version: Option<String>,
    author: Option<String>,
    #[serde(alias = "authors")]
    _authors: Option<Vec<String>>,
    description: Option<String>,
    commands: Option<serde_yaml::Value>,
}

/// Metadata extracted from fabric.mod.json (Fabric style).
#[derive(Debug, Deserialize, Default)]
struct FabricModJson {
    id: Option<String>,
    name: Option<String>,
    version: Option<String>,
    description: Option<String>,
    authors: Option<Vec<serde_json::Value>>,
}

/// Metadata extracted from mod.json (generic mod descriptor).
#[derive(Debug, Deserialize, Default)]
struct ModJson {
    name: Option<String>,
    version: Option<String>,
    author: Option<String>,
    description: Option<String>,
}

/// Run the periodic plugin scanner. Scans every `interval` seconds.
pub async fn run_plugin_scanner(
    api: Arc<ApiClient>,
    config: Arc<Config>,
    mut shutdown: broadcast::Receiver<()>,
) {
    let interval_secs = 300u64; // 5 minutes
    let data_root = format!("{}/servers", config.daemon.data_root);

    info!(interval_secs, "Plugin scanner started");

    // Initial scan so worlds/plugins appear promptly rather than after the
    // first interval elapses.
    scan_all_servers(&api, &data_root).await;

    loop {
        tokio::select! {
            _ = tokio::time::sleep(tokio::time::Duration::from_secs(interval_secs)) => {
                scan_all_servers(&api, &data_root).await;
            }
            _ = shutdown.recv() => {
                info!("Plugin scanner shutting down");
                return;
            }
        }
    }
}

/// Scan `{server_dir}/universe/worlds` for world directories and read the
/// active world from `{server_dir}/config.json` (`Defaults.World`).
fn scan_server_worlds(server_dir: &Path) -> (Vec<crate::api_client::ScannedWorld>, String) {
    let mut worlds = Vec::new();
    let worlds_dir = server_dir.join("universe").join("worlds");
    if let Ok(entries) = std::fs::read_dir(&worlds_dir) {
        for entry in entries.flatten() {
            let p = entry.path();
            if !p.is_dir() {
                continue;
            }
            if let Some(name) = p.file_name().and_then(|n| n.to_str()) {
                worlds.push(crate::api_client::ScannedWorld {
                    name: name.to_string(),
                    size_bytes: dir_size(&p),
                });
            }
        }
    }
    (worlds, read_active_world(server_dir))
}

/// Read the active world name from a server's `config.json` (`Defaults.World`).
fn read_active_world(server_dir: &Path) -> String {
    let cfg_path = server_dir.join("config.json");
    if let Ok(contents) = std::fs::read_to_string(&cfg_path) {
        if let Ok(v) = serde_json::from_str::<serde_json::Value>(&contents) {
            if let Some(w) = v
                .get("Defaults")
                .and_then(|d| d.get("World"))
                .and_then(|w| w.as_str())
            {
                return w.to_string();
            }
        }
    }
    String::new()
}

/// Recursively sum the byte size of a directory's files.
fn dir_size(path: &Path) -> i64 {
    let mut total: i64 = 0;
    if let Ok(entries) = std::fs::read_dir(path) {
        for entry in entries.flatten() {
            match entry.metadata() {
                Ok(md) if md.is_dir() => total += dir_size(&entry.path()),
                Ok(md) => total += md.len() as i64,
                Err(_) => {}
            }
        }
    }
    total
}

async fn scan_all_servers(api: &ApiClient, servers_dir: &str) {
    let path = Path::new(servers_dir);
    if !path.exists() {
        debug!("Servers directory does not exist, skipping scan");
        return;
    }

    let entries = match std::fs::read_dir(path) {
        Ok(e) => e,
        Err(err) => {
            warn!(%err, "Failed to read servers directory");
            return;
        }
    };

    for entry in entries.flatten() {
        let entry_path = entry.path();
        if !entry_path.is_dir() {
            continue;
        }

        let server_id = match entry_path.file_name().and_then(|n| n.to_str()) {
            Some(name) => name.to_string(),
            None => continue,
        };

        // Scan universe/worlds and report worlds + the active world. Done first
        // so it runs even for servers with no plugins.
        let (worlds, active_world) = scan_server_worlds(&entry_path);
        if !worlds.is_empty() {
            if let Err(err) = api.report_worlds(&server_id, &worlds, &active_world).await {
                warn!(%err, server_id = %server_id, "Failed to report worlds to API");
            }
        }

        // Scan mods/ and plugins/ directories.
        let plugins = scan_server_plugins(&entry_path);
        if plugins.is_empty() {
            continue;
        }

        info!(server_id = %server_id, count = plugins.len(), "Detected plugins");

        if let Err(err) = api.report_plugins(&server_id, &plugins).await {
            warn!(%err, server_id = %server_id, "Failed to report plugins to API");
        }
    }
}

/// Classify a directory entry's file name.
///
/// Returns the base filename (without a trailing `.disabled`) and whether the
/// file is currently enabled, or `None` if it is not a mod/plugin archive.
/// `foo.jar` → `("foo.jar", true)`, `foo.jar.disabled` → `("foo.jar", false)`.
fn classify_archive_name(filename: &str) -> Option<(String, bool)> {
    let (base, enabled) = match filename.strip_suffix(".disabled") {
        Some(base) => (base, false),
        None => (filename, true),
    };
    let lower = base.to_ascii_lowercase();
    if lower.ends_with(".jar") || lower.ends_with(".zip") {
        Some((base.to_string(), enabled))
    } else {
        None
    }
}

fn scan_server_plugins(server_dir: &Path) -> Vec<DetectedPlugin> {
    let mut plugins = Vec::new();

    for subdir in &["mods", "plugins"] {
        let dir = server_dir.join(subdir);
        if !dir.exists() || !dir.is_dir() {
            continue;
        }

        let entries = match std::fs::read_dir(&dir) {
            Ok(e) => e,
            Err(_) => continue,
        };

        for entry in entries.flatten() {
            let path = entry.path();

            let raw_name = match path.file_name().and_then(|n| n.to_str()) {
                Some(n) => n,
                None => continue,
            };

            // Disabled files keep their archive on disk under a `.disabled`
            // suffix, so they must still be reported — otherwise the panel
            // could never offer to re-enable them.
            let (filename, enabled) = match classify_archive_name(raw_name) {
                Some(v) => v,
                None => continue,
            };

            match scan_archive(&path) {
                Ok(Some(mut plugin)) => {
                    plugin.filename = filename;
                    plugin.enabled = enabled;
                    plugin.source_dir = subdir.to_string();
                    plugins.push(plugin);
                }
                Ok(None) => {
                    // No metadata found, still track it.
                    let hash = file_sha256(&path).unwrap_or_default();
                    let plugin_name = filename
                        .rsplit_once('.')
                        .map(|(stem, _ext)| stem.to_string())
                        .unwrap_or_else(|| filename.clone());
                    plugins.push(DetectedPlugin {
                        filename,
                        plugin_name,
                        version: String::new(),
                        author: String::new(),
                        description: String::new(),
                        commands: Vec::new(),
                        config_files: Vec::new(),
                        file_hash: hash,
                        enabled,
                        source_dir: subdir.to_string(),
                    });
                }
                Err(err) => {
                    debug!(?err, file = %path.display(), "Failed to scan archive");
                }
            }
        }
    }

    plugins
}

fn scan_archive(path: &Path) -> Result<Option<DetectedPlugin>> {
    let file = std::fs::File::open(path)?;
    let mut archive = zip::ZipArchive::new(file)?;

    let hash = file_sha256(path)?;
    let mut config_files = Vec::new();

    // Collect config file names.
    for i in 0..archive.len() {
        let entry = archive.by_index(i)?;
        let name = entry.name().to_string();
        if name.ends_with(".yml")
            || name.ends_with(".yaml")
            || name.ends_with(".toml")
            || name.ends_with(".json")
            || name.ends_with(".properties")
        {
            if !name.contains('/') || name.starts_with("config") {
                config_files.push(name);
            }
        }
    }

    // Try plugin.yml (Bukkit/Spigot/Paper).
    if let Ok(entry) = archive.by_name("plugin.yml") {
        let meta: PluginYml = serde_yaml::from_reader(entry).unwrap_or_default();
        let commands = extract_yaml_commands(&meta.commands);

        return Ok(Some(DetectedPlugin {
            filename: String::new(), // filled by caller
            plugin_name: meta.name.unwrap_or_default(),
            version: meta.version.unwrap_or_default(),
            author: meta.author.unwrap_or_default(),
            description: meta.description.unwrap_or_default(),
            commands,
            config_files,
            file_hash: hash,
            // Both are overwritten by the caller, which knows the directory
            // the archive was found in and whether it is `.disabled`.
            enabled: true,
            source_dir: default_source_dir(),
        }));
    }

    // Try fabric.mod.json.
    if let Ok(entry) = archive.by_name("fabric.mod.json") {
        let meta: FabricModJson = serde_json::from_reader(entry).unwrap_or_default();
        let author = meta
            .authors
            .and_then(|a| {
                a.first().map(|v| match v {
                    serde_json::Value::String(s) => s.clone(),
                    serde_json::Value::Object(o) => o
                        .get("name")
                        .and_then(|n| n.as_str())
                        .unwrap_or("")
                        .to_string(),
                    _ => String::new(),
                })
            })
            .unwrap_or_default();

        return Ok(Some(DetectedPlugin {
            filename: String::new(),
            plugin_name: meta.name.or(meta.id).unwrap_or_default(),
            version: meta.version.unwrap_or_default(),
            author,
            description: meta.description.unwrap_or_default(),
            commands: Vec::new(),
            config_files,
            file_hash: hash,
            // Both are overwritten by the caller, which knows the directory
            // the archive was found in and whether it is `.disabled`.
            enabled: true,
            source_dir: default_source_dir(),
        }));
    }

    // Try mod.json (generic).
    if let Ok(entry) = archive.by_name("mod.json") {
        let meta: ModJson = serde_json::from_reader(entry).unwrap_or_default();
        return Ok(Some(DetectedPlugin {
            filename: String::new(),
            plugin_name: meta.name.unwrap_or_default(),
            version: meta.version.unwrap_or_default(),
            author: meta.author.unwrap_or_default(),
            description: meta.description.unwrap_or_default(),
            commands: Vec::new(),
            config_files,
            file_hash: hash,
            // Both are overwritten by the caller, which knows the directory
            // the archive was found in and whether it is `.disabled`.
            enabled: true,
            source_dir: default_source_dir(),
        }));
    }

    Ok(None)
}

fn extract_yaml_commands(commands: &Option<serde_yaml::Value>) -> Vec<String> {
    match commands {
        Some(serde_yaml::Value::Mapping(map)) => map.keys().filter_map(|k| k.as_str().map(String::from)).collect(),
        _ => Vec::new(),
    }
}

fn file_sha256(path: &Path) -> Result<String> {
    let data = std::fs::read(path)?;
    let mut hasher = Sha256::new();
    hasher.update(&data);
    Ok(format!("{:x}", hasher.finalize()))
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::{AtomicU32, Ordering};

    static COUNTER: AtomicU32 = AtomicU32::new(0);

    fn scratch_dir(tag: &str) -> std::path::PathBuf {
        let n = COUNTER.fetch_add(1, Ordering::SeqCst);
        let dir = std::env::temp_dir().join(format!("talescan-test-{tag}-{}-{n}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).expect("create scratch dir");
        dir
    }

    #[test]
    fn classify_archive_name_detects_disabled_suffix() {
        assert_eq!(classify_archive_name("cool.jar"), Some(("cool.jar".into(), true)));
        assert_eq!(classify_archive_name("cool.JAR"), Some(("cool.JAR".into(), true)));
        assert_eq!(classify_archive_name("pack.zip"), Some(("pack.zip".into(), true)));
        assert_eq!(
            classify_archive_name("cool.jar.disabled"),
            Some(("cool.jar".into(), false))
        );
        assert_eq!(classify_archive_name("notes.txt"), None);
        assert_eq!(classify_archive_name("notes.txt.disabled"), None);
        assert_eq!(classify_archive_name("config"), None);
    }

    /// The 22-byte "end of central directory" record: a valid, empty zip.
    /// Enough for ZipArchive to open it and find no plugin metadata.
    const EMPTY_ZIP: &[u8] = &[
        0x50, 0x4b, 0x05, 0x06, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
    ];

    #[test]
    fn scan_server_plugins_reports_source_dir_and_enabled_state() {
        let server_dir = scratch_dir("scan");
        std::fs::create_dir_all(server_dir.join("mods")).unwrap();
        std::fs::create_dir_all(server_dir.join("plugins")).unwrap();
        std::fs::write(server_dir.join("mods").join("alpha.jar"), EMPTY_ZIP).unwrap();
        std::fs::write(server_dir.join("plugins").join("beta.jar.disabled"), EMPTY_ZIP).unwrap();
        std::fs::write(server_dir.join("plugins").join("readme.txt"), b"ignore me").unwrap();

        let found = scan_server_plugins(&server_dir);
        assert_eq!(found.len(), 2, "only archives are reported: {found:?}");

        let alpha = found.iter().find(|p| p.filename == "alpha.jar").expect("alpha.jar");
        assert!(alpha.enabled);
        assert_eq!(alpha.source_dir, "mods");

        // Reported under its base name, not "beta.jar.disabled".
        let beta = found.iter().find(|p| p.filename == "beta.jar").expect("beta.jar");
        assert!(!beta.enabled, "a .disabled file must report enabled=false");
        assert_eq!(beta.source_dir, "plugins");

        let _ = std::fs::remove_dir_all(&server_dir);
    }
}
