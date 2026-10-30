use anyhow::{Context, Result};
use serde::{Deserialize, Serialize};
use std::path::Path;
use tracing::debug;

/// Top-level configuration loaded from a TOML file or environment variables.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Config {
    pub daemon: DaemonConfig,
    pub resources: ResourcesConfig,
    #[serde(default)]
    pub hytale: HytaleConfig,
}

/// Hytale server runtime settings.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct HytaleConfig {
    /// Path to the Java binary. Defaults to "java" (must be on PATH).
    #[serde(default = "HytaleConfig::default_java_binary")]
    pub java_binary: String,

    /// Path to the server JAR, relative to the server's data_path.
    /// Expected layout: {data_path}/Server/HytaleServer.jar
    #[serde(default = "HytaleConfig::default_server_jar")]
    pub server_jar: String,

    /// Maximum seconds to wait for the server to log a ready signal on startup.
    #[serde(default = "HytaleConfig::default_startup_timeout_s")]
    pub startup_timeout_s: u64,

    /// Maximum seconds to wait for graceful stop before force-killing.
    #[serde(default = "HytaleConfig::default_stop_timeout_s")]
    pub stop_timeout_s: u64,

    /// Extra JVM arguments injected before `-jar` (e.g. GC flags).
    #[serde(default)]
    pub extra_jvm_args: Vec<String>,

    /// Extra arguments appended after the JAR path (e.g. feature flags).
    #[serde(default)]
    pub extra_server_args: Vec<String>,
}

impl Default for HytaleConfig {
    fn default() -> Self {
        Self {
            java_binary: Self::default_java_binary(),
            server_jar: Self::default_server_jar(),
            startup_timeout_s: Self::default_startup_timeout_s(),
            stop_timeout_s: Self::default_stop_timeout_s(),
            extra_jvm_args: vec![],
            extra_server_args: vec![],
        }
    }
}

impl HytaleConfig {
    fn default_java_binary() -> String { "java".to_string() }
    fn default_server_jar() -> String { "Server/HytaleServer.jar".to_string() }
    fn default_startup_timeout_s() -> u64 { 120 }
    fn default_stop_timeout_s() -> u64 { 30 }
}

/// Core daemon identity and connectivity settings.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DaemonConfig {
    /// Unique identifier for this node (UUID string).
    pub node_id: String,

    /// Base URL of the TalePanel API, e.g. "http://panel.example.com:8080".
    pub api_url: String,

    /// Registration token issued by TalePanel for this node.
    pub node_token: String,

    /// Port for the daemon's local HTTP health/status API.
    #[serde(default = "DaemonConfig::default_listen_port")]
    pub listen_port: u16,

    /// Root directory where server data directories are stored.
    #[serde(default = "DaemonConfig::default_data_root")]
    pub data_root: String,

    /// Log level filter string, e.g. "info", "debug", "warn".
    #[serde(default = "DaemonConfig::default_log_level")]
    pub log_level: String,

    /// Runtime environment: "development" or "production".
    /// Controls log format (pretty vs JSON).
    #[serde(default = "DaemonConfig::default_env")]
    pub env: String,

    /// Skip TLS certificate verification when calling the panel API.
    /// Set to true only when the panel is reachable via a self-signed cert
    /// (e.g. an --no-domain panel install over a trusted network).
    #[serde(default)]
    pub insecure_tls: bool,

    /// Allow provisioning to fall back to a fake "dev-mode" server stub when
    /// the real Hytale binaries cannot be obtained.
    ///
    /// Off by default and ignored in production: a stub server *looks* like it
    /// runs but hosts nothing, which is worse than a clear provisioning error.
    /// Only TalePanel's own developers should ever switch this on.
    #[serde(default)]
    pub allow_dev_stub: bool,

    /// Directory holding this node's mTLS material (`ca.pem`, `node-cert.pem`,
    /// `node-key.pem`). Empty in the config file means `{data_root}/pki`;
    /// `Config::from_file` / `Config::from_env` resolve it.
    #[serde(default)]
    pub pki_dir: String,

    /// Keep serving the legacy plaintext HTTP + bearer-token API alongside the
    /// mTLS listener (same port, first-byte sniffing). On during the
    /// transition so panels that do not speak mTLS yet keep working.
    #[serde(default = "DaemonConfig::default_allow_legacy_bearer")]
    pub allow_legacy_bearer: bool,

    /// Refuse every connection that is not mutually authenticated TLS with a
    /// panel-CA client certificate. Overrides `allow_legacy_bearer`.
    #[serde(default)]
    pub require_mtls: bool,
}

impl DaemonConfig {
    fn default_listen_port() -> u16 {
        8444
    }
    fn default_data_root() -> String {
        "/srv/taledaemon".to_string()
    }
    fn default_log_level() -> String {
        "info".to_string()
    }
    fn default_env() -> String {
        "development".to_string()
    }
    fn default_allow_legacy_bearer() -> bool {
        true
    }
    fn default_pki_dir(data_root: &str) -> String {
        format!("{}/pki", data_root.trim_end_matches('/'))
    }
}

/// Resource limits and polling intervals.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ResourcesConfig {
    /// Maximum number of concurrent Hytale server processes on this node.
    #[serde(default = "ResourcesConfig::default_max_servers")]
    pub max_servers: u32,

    /// How often to collect internal metrics, in milliseconds.
    #[serde(default = "ResourcesConfig::default_metrics_interval_ms")]
    pub metrics_interval_ms: u64,

    /// How often to send a heartbeat to the TalePanel API, in seconds.
    #[serde(default = "ResourcesConfig::default_heartbeat_interval_s")]
    pub heartbeat_interval_s: u64,

    /// How often to poll for pending commands from the TalePanel API, in seconds.
    #[serde(default = "ResourcesConfig::default_command_poll_interval_s")]
    pub command_poll_interval_s: u64,
}

impl ResourcesConfig {
    fn default_max_servers() -> u32 {
        20
    }
    fn default_metrics_interval_ms() -> u64 {
        5000
    }
    fn default_heartbeat_interval_s() -> u64 {
        10
    }
    fn default_command_poll_interval_s() -> u64 {
        2
    }
}

impl Default for ResourcesConfig {
    fn default() -> Self {
        Self {
            max_servers: Self::default_max_servers(),
            metrics_interval_ms: Self::default_metrics_interval_ms(),
            heartbeat_interval_s: Self::default_heartbeat_interval_s(),
            command_poll_interval_s: Self::default_command_poll_interval_s(),
        }
    }
}

impl Config {
    /// Load configuration using the following priority order:
    /// 1. `/etc/taledaemon/config.toml`
    /// 2. `./config.toml` (current working directory)
    /// 3. Environment variables (`TALEDAEMON_*`)
    #[tracing::instrument]
    pub fn load() -> Result<Self> {
        let candidates = ["/etc/taledaemon/config.toml", "./config.toml"];

        for path in &candidates {
            if Path::new(path).exists() {
                debug!(path, "Loading config from file");
                return Self::from_file(path)
                    .with_context(|| format!("Failed to parse config file: {path}"));
            }
        }

        debug!("No config file found; falling back to environment variables");
        Self::from_env().context("Failed to load config from environment variables")
    }

    /// Load configuration from a TOML file at the given path.
    #[tracing::instrument(skip_all, fields(path = %path))]
    pub fn from_file(path: &str) -> Result<Self> {
        let contents = std::fs::read_to_string(path)
            .with_context(|| format!("Cannot read config file: {path}"))?;

        let mut cfg: Self = toml::from_str(&contents)
            .with_context(|| format!("Invalid TOML in config file: {path}"))?;
        if cfg.daemon.pki_dir.is_empty() {
            cfg.daemon.pki_dir = DaemonConfig::default_pki_dir(&cfg.daemon.data_root);
        }
        Ok(cfg)
    }

    /// Load configuration entirely from `TALEDAEMON_*` environment variables.
    ///
    /// Required variables:
    ///   TALEDAEMON_NODE_ID
    ///   TALEDAEMON_API_URL
    ///   TALEDAEMON_NODE_TOKEN
    ///
    /// Optional variables (all others use defaults):
    ///   TALEDAEMON_LISTEN_PORT
    ///   TALEDAEMON_DATA_ROOT
    ///   TALEDAEMON_LOG_LEVEL
    ///   TALEDAEMON_ENV
    ///   TALEDAEMON_MAX_SERVERS
    ///   TALEDAEMON_METRICS_INTERVAL_MS
    ///   TALEDAEMON_HEARTBEAT_INTERVAL_S
    ///   TALEDAEMON_COMMAND_POLL_INTERVAL_S
    ///   TALEDAEMON_PKI_DIR              (default `{data_root}/pki`)
    ///   TALEDAEMON_ALLOW_LEGACY_BEARER  (default true)
    ///   TALEDAEMON_REQUIRE_MTLS         (default false)
    pub fn from_env() -> Result<Self> {
        let node_id = std::env::var("TALEDAEMON_NODE_ID")
            .context("TALEDAEMON_NODE_ID is required when no config file is present")?;
        let api_url = std::env::var("TALEDAEMON_API_URL")
            .context("TALEDAEMON_API_URL is required when no config file is present")?;
        let node_token = std::env::var("TALEDAEMON_NODE_TOKEN")
            .context("TALEDAEMON_NODE_TOKEN is required when no config file is present")?;

        let listen_port = std::env::var("TALEDAEMON_LISTEN_PORT")
            .ok()
            .and_then(|v| v.parse::<u16>().ok())
            .unwrap_or(DaemonConfig::default_listen_port());

        let data_root = std::env::var("TALEDAEMON_DATA_ROOT")
            .unwrap_or_else(|_| DaemonConfig::default_data_root());

        let log_level = std::env::var("TALEDAEMON_LOG_LEVEL")
            .unwrap_or_else(|_| DaemonConfig::default_log_level());

        let env = std::env::var("TALEDAEMON_ENV").unwrap_or_else(|_| DaemonConfig::default_env());

        let max_servers = std::env::var("TALEDAEMON_MAX_SERVERS")
            .ok()
            .and_then(|v| v.parse::<u32>().ok())
            .unwrap_or(ResourcesConfig::default_max_servers());

        let metrics_interval_ms = std::env::var("TALEDAEMON_METRICS_INTERVAL_MS")
            .ok()
            .and_then(|v| v.parse::<u64>().ok())
            .unwrap_or(ResourcesConfig::default_metrics_interval_ms());

        let heartbeat_interval_s = std::env::var("TALEDAEMON_HEARTBEAT_INTERVAL_S")
            .ok()
            .and_then(|v| v.parse::<u64>().ok())
            .unwrap_or(ResourcesConfig::default_heartbeat_interval_s());

        let command_poll_interval_s = std::env::var("TALEDAEMON_COMMAND_POLL_INTERVAL_S")
            .ok()
            .and_then(|v| v.parse::<u64>().ok())
            .unwrap_or(ResourcesConfig::default_command_poll_interval_s());

        let insecure_tls = std::env::var("TALEDAEMON_INSECURE_TLS")
            .map(|v| matches!(v.as_str(), "1" | "true" | "TRUE" | "yes"))
            .unwrap_or(false);

        let allow_dev_stub = std::env::var("TALEDAEMON_ALLOW_DEV_STUB")
            .map(|v| matches!(v.as_str(), "1" | "true" | "TRUE" | "yes"))
            .unwrap_or(false);

        let pki_dir = std::env::var("TALEDAEMON_PKI_DIR")
            .ok()
            .filter(|v| !v.is_empty())
            .unwrap_or_else(|| DaemonConfig::default_pki_dir(&data_root));

        let allow_legacy_bearer = std::env::var("TALEDAEMON_ALLOW_LEGACY_BEARER")
            .map(|v| matches!(v.as_str(), "1" | "true" | "TRUE" | "yes"))
            .unwrap_or(DaemonConfig::default_allow_legacy_bearer());

        let require_mtls = std::env::var("TALEDAEMON_REQUIRE_MTLS")
            .map(|v| matches!(v.as_str(), "1" | "true" | "TRUE" | "yes"))
            .unwrap_or(false);

        Ok(Config {
            daemon: DaemonConfig {
                node_id,
                api_url,
                node_token,
                listen_port,
                data_root,
                log_level,
                env,
                insecure_tls,
                allow_dev_stub,
                pki_dir,
                allow_legacy_bearer,
                require_mtls,
            },
            resources: ResourcesConfig {
                max_servers,
                metrics_interval_ms,
                heartbeat_interval_s,
                command_poll_interval_s,
            },
            hytale: HytaleConfig::default(),
        })
    }

    /// Returns true if the daemon is configured to run in production mode.
    pub fn is_production(&self) -> bool {
        self.daemon.env.eq_ignore_ascii_case("production")
    }

    /// Returns true only if the operator has explicitly opted in to the fake
    /// dev-mode server stub *and* the daemon is not running in production.
    /// Everywhere else a failed download is a hard provisioning error.
    pub fn dev_stub_allowed(&self) -> bool {
        self.daemon.allow_dev_stub && !self.is_production()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn cfg(env: &str, allow_dev_stub: bool) -> Config {
        Config {
            daemon: DaemonConfig {
                node_id: "node".into(),
                api_url: "https://panel.example".into(),
                node_token: "token".into(),
                listen_port: DaemonConfig::default_listen_port(),
                data_root: DaemonConfig::default_data_root(),
                log_level: DaemonConfig::default_log_level(),
                env: env.into(),
                insecure_tls: false,
                allow_dev_stub,
                pki_dir: DaemonConfig::default_pki_dir(&DaemonConfig::default_data_root()),
                allow_legacy_bearer: DaemonConfig::default_allow_legacy_bearer(),
                require_mtls: false,
            },
            resources: ResourcesConfig::default(),
            hytale: HytaleConfig::default(),
        }
    }

    #[test]
    fn dev_stub_is_off_unless_explicitly_enabled_outside_production() {
        // The dangerous default: no flag set at all.
        assert!(!cfg("development", false).dev_stub_allowed());
        assert!(!cfg("production", false).dev_stub_allowed());
        // Opted in, but production still refuses.
        assert!(!cfg("production", true).dev_stub_allowed());
        assert!(!cfg("PRODUCTION", true).dev_stub_allowed());
        // The only combination that serves a fake server.
        assert!(cfg("development", true).dev_stub_allowed());
    }

    #[test]
    fn mtls_settings_default_to_transition_mode() {
        let dir = std::env::temp_dir().join(format!("tp-cfg-test-{}", std::process::id()));
        std::fs::create_dir_all(&dir).unwrap();
        let path = dir.join("config.toml");
        std::fs::write(
            &path,
            "[daemon]
node_id = \"n\"
api_url = \"http://p\"
node_token = \"t\"
data_root = \"/data/td/\"
[resources]
",
        )
        .unwrap();
        let c = Config::from_file(path.to_str().unwrap()).unwrap();
        assert_eq!(c.daemon.pki_dir, "/data/td/pki");
        assert!(c.daemon.allow_legacy_bearer);
        assert!(!c.daemon.require_mtls);

        std::fs::write(
            &path,
            "[daemon]
node_id = \"n\"
api_url = \"http://p\"
node_token = \"t\"
pki_dir = \"/etc/td/pki\"
allow_legacy_bearer = false
require_mtls = true
[resources]
",
        )
        .unwrap();
        let c = Config::from_file(path.to_str().unwrap()).unwrap();
        assert_eq!(c.daemon.pki_dir, "/etc/td/pki");
        assert!(!c.daemon.allow_legacy_bearer);
        assert!(c.daemon.require_mtls);
        let _ = std::fs::remove_dir_all(&dir);
    }
}
