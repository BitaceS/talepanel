use anyhow::{bail, Context, Result};
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use tracing::{debug, instrument, warn};

// ---------------------------------------------------------------------------
// Request / response types
// ---------------------------------------------------------------------------

/// Payload sent when registering this node with the TalePanel API.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct NodeRegistration {
    pub node_id: String,
    /// Number of logical CPU cores available on this host.
    pub cpu_cores: u32,
    /// Total physical RAM in megabytes.
    pub total_ram_mb: u64,
    /// Total disk space (on the data root partition) in megabytes.
    pub total_disk_mb: u64,
    /// Daemon software version string.
    pub version: String,
}

/// Snapshot metrics bundled with every heartbeat.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct HeartbeatMetrics {
    /// Average CPU utilisation across all cores, as a percentage (0.0–100.0).
    pub cpu_percent: f32,
    /// RAM currently in use on the host, in megabytes.
    pub ram_used_mb: u64,
    /// Disk space currently in use on the data root partition, in megabytes.
    pub disk_used_mb: u64,
    /// Number of Hytale server processes currently managed by this daemon.
    pub server_count: usize,
    /// UTC timestamp of this heartbeat.
    pub timestamp: DateTime<Utc>,
}

/// A command issued by the TalePanel API for the daemon to execute.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DaemonCommand {
    /// Unique command identifier (UUID).
    pub id: String,
    /// The server this command targets.
    pub server_id: String,
    /// Discriminant string: "start" | "stop" | "restart" | "kill" | "send_command".
    pub command_type: String,
    /// Arbitrary JSON payload; shape depends on `command_type`.
    pub payload: serde_json::Value,
}

/// Acknowledgement sent back to the API after processing a command.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CommandResult {
    /// Whether the command was executed without error.
    pub success: bool,
    /// Human-readable success output (if any).
    #[serde(skip_serializing_if = "Option::is_none")]
    pub output: Option<String>,
    /// Human-readable error description (if not successful).
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
}

impl CommandResult {
    pub fn ok(output: impl Into<String>) -> Self {
        Self {
            success: true,
            output: Some(output.into()),
            error: None,
        }
    }

    pub fn err(error: impl Into<String>) -> Self {
        Self {
            success: false,
            output: None,
            error: Some(error.into()),
        }
    }
}

/// A single line of log output produced by a managed server process.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LogLine {
    pub timestamp: DateTime<Utc>,
    /// Severity level: "INFO" | "WARN" | "ERROR".
    pub level: String,
    pub message: String,
}

// ---------------------------------------------------------------------------
// Internal request bodies
// ---------------------------------------------------------------------------

#[derive(Serialize)]
struct HeartbeatBody<'a> {
    status: &'a str,
    cpu_percent: f32,
    ram_used_mb: u64,
    disk_used_mb: u64,
    server_count: usize,
    timestamp: DateTime<Utc>,
    /// Included so the API can (re-)register the daemon HTTP client pool entry
    /// on every heartbeat without requiring a separate handshake.
    node_token: &'a str,
}

#[derive(Serialize)]
struct ServerStatusBody<'a> {
    status: &'a str,
}

// ---------------------------------------------------------------------------
// ApiClient
// ---------------------------------------------------------------------------

/// HTTP client for all communication with the TalePanel control-plane API.
#[derive(Clone, Debug)]
pub struct ApiClient {
    base_url: String,
    node_token: String,
    node_id: String,
    http: reqwest::Client,
}

impl ApiClient {
    /// Construct a new `ApiClient`.
    ///
    /// * `base_url`   – e.g. `"http://panel.example.com:8080"` (no trailing slash)
    /// * `node_token` – Bearer token issued by TalePanel for this node
    /// * `node_id`    – UUID string identifying this node
    pub fn new(base_url: impl Into<String>, node_token: impl Into<String>, node_id: impl Into<String>, insecure_tls: bool) -> Self {
        let http = reqwest::Client::builder()
            .timeout(std::time::Duration::from_secs(30))
            .user_agent("TaleDaemon/0.1.0")
            .danger_accept_invalid_certs(insecure_tls)
            .build()
            .expect("Failed to build reqwest client");

        Self {
            base_url: base_url.into().trim_end_matches('/').to_string(),
            node_token: node_token.into(),
            node_id: node_id.into(),
            http,
        }
    }

    /// Build the `Authorization: Bearer …` header value.
    fn auth_header(&self) -> String {
        format!("Bearer {}", self.node_token)
    }

    // -----------------------------------------------------------------------
    // Public API
    // -----------------------------------------------------------------------

    /// Register this node with the TalePanel API.
    ///
    /// Called once on startup. If the node is already known the API is
    /// expected to return 200/204; a 4xx response is treated as a fatal error.
    #[instrument(skip(self, node_info), fields(node_id = %self.node_id))]
    pub async fn register(&self, node_info: NodeRegistration) -> Result<()> {
        let url = format!("{}/api/v1/nodes/{}/register", self.base_url, self.node_id);
        debug!(%url, "Registering node");

        let resp = self
            .http
            .post(&url)
            .header("Authorization", self.auth_header())
            .json(&node_info)
            .send()
            .await
            .context("HTTP request failed for node registration")?;

        let status = resp.status();
        if !status.is_success() {
            let body = resp.text().await.unwrap_or_default();
            bail!("Node registration failed: HTTP {status} – {body}");
        }

        debug!("Node registration succeeded");
        Ok(())
    }

    /// Send a heartbeat to the TalePanel API.
    ///
    /// The `status` string should be one of: `"online"`, `"degraded"`, `"offline"`.
    #[instrument(skip(self, metrics), fields(node_id = %self.node_id, status))]
    pub async fn heartbeat(&self, status: &str, metrics: HeartbeatMetrics) -> Result<()> {
        let url = format!("{}/api/v1/nodes/{}/heartbeat", self.base_url, self.node_id);
        debug!(%url, "Sending heartbeat");

        let body = HeartbeatBody {
            status,
            cpu_percent: metrics.cpu_percent,
            ram_used_mb: metrics.ram_used_mb,
            disk_used_mb: metrics.disk_used_mb,
            server_count: metrics.server_count,
            timestamp: metrics.timestamp,
            node_token: &self.node_token,
        };

        let resp = self
            .http
            .post(&url)
            .header("Authorization", self.auth_header())
            .json(&body)
            .send()
            .await
            .context("HTTP request failed for heartbeat")?;

        let status_code = resp.status();
        if !status_code.is_success() {
            let text = resp.text().await.unwrap_or_default();
            // Heartbeat failures are non-fatal; we warn but do not bail.
            warn!(%status_code, response_body = %text, "Heartbeat HTTP error");
        }

        Ok(())
    }

    /// Fetch the list of commands that the TalePanel API wants this node to
    /// execute. Returns an empty `Vec` when there are no pending commands.
    #[instrument(skip(self), fields(node_id = %self.node_id))]
    pub async fn get_pending_commands(&self) -> Result<Vec<DaemonCommand>> {
        let url = format!(
            "{}/api/v1/nodes/{}/commands/pending",
            self.base_url, self.node_id
        );
        debug!(%url, "Polling for pending commands");

        let resp = self
            .http
            .get(&url)
            .header("Authorization", self.auth_header())
            .send()
            .await
            .context("HTTP request failed for get_pending_commands")?;

        let status = resp.status();
        if !status.is_success() {
            let body = resp.text().await.unwrap_or_default();
            bail!("get_pending_commands failed: HTTP {status} – {body}");
        }

        let commands: Vec<DaemonCommand> = resp
            .json()
            .await
            .context("Failed to deserialize pending commands response")?;

        debug!(count = commands.len(), "Received pending commands");
        Ok(commands)
    }

    /// Acknowledge that a command has been processed and report its result.
    #[instrument(skip(self, result), fields(node_id = %self.node_id, command_id))]
    pub async fn ack_command(&self, command_id: &str, result: CommandResult) -> Result<()> {
        let url = format!(
            "{}/api/v1/nodes/{}/commands/{}/ack",
            self.base_url, self.node_id, command_id
        );
        debug!(%url, success = result.success, "Acknowledging command");

        let resp = self
            .http
            .post(&url)
            .header("Authorization", self.auth_header())
            .json(&result)
            .send()
            .await
            .context("HTTP request failed for ack_command")?;

        let status = resp.status();
        if !status.is_success() {
            let body = resp.text().await.unwrap_or_default();
            bail!("ack_command failed: HTTP {status} – {body}");
        }

        Ok(())
    }

    /// Report the lifecycle status of a managed server to the TalePanel API.
    ///
    /// `status` should be one of: `"starting"`, `"running"`, `"stopping"`,
    /// `"stopped"`, `"crashed"`.
    #[instrument(skip(self), fields(server_id, status))]
    pub async fn report_server_status(&self, server_id: &str, status: &str) -> Result<()> {
        let url = format!(
            "{}/api/v1/servers/{}/daemon/status",
            self.base_url, server_id
        );
        debug!(%url, %status, "Reporting server status");

        let body = ServerStatusBody { status };

        let resp = self
            .http
            .post(&url)
            .header("Authorization", self.auth_header())
            .json(&body)
            .send()
            .await
            .context("HTTP request failed for report_server_status")?;

        let status_code = resp.status();
        if !status_code.is_success() {
            let text = resp.text().await.unwrap_or_default();
            warn!(%status_code, response_body = %text, "report_server_status HTTP error (non-fatal)");
        }

        Ok(())
    }

    /// Report detected plugins for a server to the TalePanel API.
    #[instrument(skip(self, plugins), fields(server_id, plugin_count = plugins.len()))]
    pub async fn report_plugins(&self, server_id: &str, plugins: &[crate::plugin_scanner::DetectedPlugin]) -> Result<()> {
        let url = format!(
            "{}/api/v1/servers/{}/daemon/plugins",
            self.base_url, server_id
        );
        debug!(%url, count = plugins.len(), "Reporting detected plugins");

        let resp = self
            .http
            .post(&url)
            .header("Authorization", self.auth_header())
            .json(plugins)
            .send()
            .await
            .context("HTTP request failed for report_plugins")?;

        let status = resp.status();
        if !status.is_success() {
            let body = resp.text().await.unwrap_or_default();
            warn!(%status, response_body = %body, "report_plugins HTTP error (non-fatal)");
        }

        Ok(())
    }

    /// Push a batch of log lines from a managed server to the TalePanel API.
    ///
    /// The API is expected to fan these out to any subscribed WebSocket clients
    /// in the panel frontend.
    #[instrument(skip(self, lines), fields(server_id, line_count = lines.len()))]
    pub async fn push_log_lines(&self, server_id: &str, lines: Vec<LogLine>) -> Result<()> {
        if lines.is_empty() {
            return Ok(());
        }

        let url = format!("{}/api/v1/servers/{}/daemon/logs", self.base_url, server_id);
        debug!(%url, count = lines.len(), "Pushing log lines");

        let resp = self
            .http
            .post(&url)
            .header("Authorization", self.auth_header())
            .json(&lines)
            .send()
            .await
            .context("HTTP request failed for push_log_lines")?;

        let status = resp.status();
        if !status.is_success() {
            let body = resp.text().await.unwrap_or_default();
            warn!(%status, response_body = %body, "push_log_lines HTTP error (non-fatal)");
        }

        Ok(())
    }
}
