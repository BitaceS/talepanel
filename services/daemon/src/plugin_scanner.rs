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
    pub filename: String,
    pub plugin_name: String,
    pub version: String,
    pub author: String,
    pub description: String,
    pub commands: Vec<String>,
    pub config_files: Vec<String>,
    pub file_hash: String,
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
            let ext = path.extension().and_then(|e| e.to_str()).unwrap_or("");

            if ext != "jar" && ext != "zip" {
                continue;
            }

            let filename = match path.file_name().and_then(|n| n.to_str()) {
                Some(n) => n.to_string(),
                None => continue,
            };

            match scan_archive(&path) {
                Ok(Some(mut plugin)) => {
                    plugin.filename = filename;
                    plugins.push(plugin);
                }
                Ok(None) => {
                    // No metadata found, still track it.
                    let hash = file_sha256(&path).unwrap_or_default();
                    plugins.push(DetectedPlugin {
                        filename,
                        plugin_name: path
                            .file_stem()
                            .and_then(|s| s.to_str())
                            .unwrap_or("unknown")
                            .to_string(),
                        version: String::new(),
                        author: String::new(),
                        description: String::new(),
                        commands: Vec::new(),
                        config_files: Vec::new(),
                        file_hash: hash,
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
