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
    /// Set only when the local listener really serves TLS with the node cert.
    pub mtls: Option<MtlsAdvert>,
    /// Per-server usage of the running servers, for the panel's performance
    /// graphs. Panels older than v0.14 ignore it.
    pub servers: Vec<ServerSample>,
    /// Which isolation layers are active (panel v0.16+ shows them).
    pub isolation: Option<crate::sandbox::Status>,
}

/// One running server's resource usage in a heartbeat.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct ServerSample {
    pub server_id: String,
    /// Process CPU since the previous sample; 100 = one full core.
    pub cpu_percent: f32,
    pub ram_mb: u64,
}

/// Extract the certificate hint from a heartbeat answer. Older panels send
/// none; a malformed one is ignored rather than failing the heartbeat.
fn parse_heartbeat_hint(body: &[u8]) -> Option<PkiHint> {
    #[derive(Deserialize)]
    struct Answer {
        #[serde(default)]
        pki: Option<PkiHint>,
    }
    serde_json::from_slice::<Answer>(body).ok()?.pki
}

/// Node certificate details advertised in the heartbeat so the panel can
/// switch to https + mTLS for this node.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct MtlsAdvert {
    /// Certificate serial, lowercase hex.
    pub serial: String,
    pub not_after: DateTime<Utc>,
}

/// What the panel answers to a CSR.
#[derive(Debug, Clone, Deserialize)]
pub struct NodeCertResponse {
    /// Trust bundle: the CA that signed `node_cert_pem` first, then any CA
    /// still trusted while a rotation runs.
    #[serde(rename = "ca_cert_pem")]
    pub ca_pem: String,
    #[serde(rename = "node_cert_pem")]
    pub cert_pem: String,
    /// Cross certificates that prove a new CA in the bundle to a node that
    /// trusts an older one (panel v0.15+; empty otherwise).
    #[serde(rename = "ca_endorsements_pem", default)]
    pub endorsements_pem: String,
}

/// Certificate hint in a heartbeat answer (panel v0.15+).
#[derive(Debug, Clone, Default, PartialEq, Eq, Deserialize)]
pub struct PkiHint {
    /// CAs the node should trust from now on.
    pub ca_bundle_pem: String,
    #[serde(default)]
    pub endorsements_pem: String,
    /// The node's certificate is not from the panel's active CA.
    #[serde(default)]
    pub renew: bool,
}

/// The panel has no CSR endpoint (older panel): 404 or 405. Not retryable.
#[derive(Debug)]
pub struct CsrUnsupported(pub reqwest::StatusCode);

impl std::fmt::Display for CsrUnsupported {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(
            f,
            "panel does not support node CSR enrollment (HTTP {})",
            self.0
        )
    }
}

impl std::error::Error for CsrUnsupported {}

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

/// A world discovered on disk under `{data_path}/universe/worlds`.
#[derive(Debug, Clone, Serialize)]
pub struct ScannedWorld {
    pub name: String,
    pub size_bytes: i64,
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
    #[serde(skip_serializing_if = "Option::is_none")]
    mtls_ready: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    cert_serial: Option<&'a str>,
    #[serde(skip_serializing_if = "Option::is_none")]
    cert_not_after: Option<DateTime<Utc>>,
    /// This daemon renews its certificate while running and follows panel
    /// CA rotations, so the panel may send it certificate hints.
    #[serde(skip_serializing_if = "Option::is_none")]
    cert_renewal: Option<bool>,
    #[serde(skip_serializing_if = "<[ServerSample]>::is_empty")]
    servers: &'a [ServerSample],
    #[serde(skip_serializing_if = "Option::is_none")]
    isolation: Option<crate::sandbox::Status>,
}

impl<'a> HeartbeatBody<'a> {
    fn new(status: &'a str, metrics: &'a HeartbeatMetrics, node_token: &'a str) -> Self {
        Self {
            status,
            cpu_percent: metrics.cpu_percent,
            ram_used_mb: metrics.ram_used_mb,
            disk_used_mb: metrics.disk_used_mb,
            server_count: metrics.server_count,
            timestamp: metrics.timestamp,
            node_token,
            mtls_ready: metrics.mtls.as_ref().map(|_| true),
            cert_serial: metrics.mtls.as_ref().map(|m| m.serial.as_str()),
            cert_not_after: metrics.mtls.as_ref().map(|m| m.not_after),
            cert_renewal: metrics.mtls.as_ref().map(|_| true),
            servers: &metrics.servers,
            isolation: metrics.isolation,
        }
    }
}

#[derive(Serialize)]
struct ServerStatusBody<'a> {
    status: &'a str,
    /// Process exit code on an unexpected exit (status "crashed").
    #[serde(skip_serializing_if = "Option::is_none")]
    exit_code: Option<i32>,
    /// With "stopped" after provisioning: what provisioning did, so the
    /// panel knows whether a new server build is in place.
    #[serde(skip_serializing_if = "Option::is_none")]
    provision_result: Option<&'a str>,
    /// The build the downloader reported, if any.
    #[serde(skip_serializing_if = "Option::is_none")]
    build: Option<&'a str>,
}

/// What a provisioning run did with the server files.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ProvisionResult {
    /// New files from the Hytale Downloader.
    Downloaded,
    /// The server files were already there; nothing was downloaded.
    Kept,
    /// Copied from the operator's local `hytale-bin` (build unknown).
    Local,
    /// A reinstall got no new files; the previous ones were put back.
    Restored,
    /// Dev-mode stub, not a real server.
    Stub,
}

impl ProvisionResult {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Downloaded => "downloaded",
            Self::Kept => "kept",
            Self::Local => "local",
            Self::Restored => "restored",
            Self::Stub => "stub",
        }
    }
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
    pub fn new(
        base_url: impl Into<String>,
        node_token: impl Into<String>,
        node_id: impl Into<String>,
        insecure_tls: bool,
    ) -> Self {
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

    /// Submit a PEM CSR to the panel CA and receive the signed node cert.
    ///
    /// Returns `(ca_cert_pem, node_cert_pem)`.
    #[instrument(skip(self, csr_pem), fields(node_id = %self.node_id))]
    pub async fn request_node_cert(&self, csr_pem: &str) -> Result<NodeCertResponse> {
        let url = format!("{}/api/v1/nodes/{}/csr", self.base_url, self.node_id);
        debug!(%url, "Requesting node certificate");

        let resp = self
            .http
            .post(&url)
            .header("Authorization", self.auth_header())
            .json(&serde_json::json!({ "csr_pem": csr_pem }))
            .timeout(std::time::Duration::from_secs(10))
            .send()
            .await
            .context("HTTP request failed for node CSR")?;

        let status = resp.status();
        if status == reqwest::StatusCode::NOT_FOUND
            || status == reqwest::StatusCode::METHOD_NOT_ALLOWED
        {
            return Err(CsrUnsupported(status).into());
        }
        if !status.is_success() {
            let body = resp.text().await.unwrap_or_default();
            bail!("Node CSR request failed: HTTP {status} – {body}");
        }

        resp.json().await.context("Invalid CSR response body")
    }

    /// Send a heartbeat to the TalePanel API.
    ///
    /// The `status` string should be one of: `"online"`, `"degraded"`, `"offline"`.
    #[instrument(skip(self, metrics), fields(node_id = %self.node_id, status))]
    ///
    /// Returns the panel's certificate hint, if it sent one.
    pub async fn heartbeat(
        &self,
        status: &str,
        metrics: HeartbeatMetrics,
    ) -> Result<Option<PkiHint>> {
        let url = format!("{}/api/v1/nodes/{}/heartbeat", self.base_url, self.node_id);
        debug!(%url, "Sending heartbeat");

        let body = HeartbeatBody::new(status, &metrics, &self.node_token);

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
            return Ok(None);
        }

        Ok(parse_heartbeat_hint(
            &resp.bytes().await.unwrap_or_default(),
        ))
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
        self.report_server_status_with_exit(server_id, status, None)
            .await
    }

    /// Like `report_server_status`, plus the process exit code of a crash so
    /// the panel can react to Hytale's meaningful exit codes (8, 11).
    pub async fn report_server_status_with_exit(
        &self,
        server_id: &str,
        status: &str,
        exit_code: Option<i32>,
    ) -> Result<()> {
        debug!(%server_id, %status, ?exit_code, "Reporting server status");
        let body = ServerStatusBody {
            status,
            exit_code,
            provision_result: None,
            build: None,
        };
        self.post_server_status(server_id, &body).await
    }

    /// Reports the end of a successful provisioning run ("stopped") with
    /// what it did and, if known, the installed build.
    pub async fn report_provisioned(
        &self,
        server_id: &str,
        result: ProvisionResult,
        build: Option<&str>,
    ) -> Result<()> {
        let body = ServerStatusBody {
            status: "stopped",
            exit_code: None,
            provision_result: Some(result.as_str()),
            build,
        };
        self.post_server_status(server_id, &body).await
    }

    async fn post_server_status(&self, server_id: &str, body: &ServerStatusBody<'_>) -> Result<()> {
        let url = format!(
            "{}/api/v1/servers/{}/daemon/status",
            self.base_url, server_id
        );

        let resp = self
            .http
            .post(&url)
            .header("Authorization", self.auth_header())
            .json(body)
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

    /// Report a detected DDoS event for this node to the TalePanel API, which
    /// fires any matching `ddos` alert rules. Fire-and-forget; non-2xx is logged.
    #[instrument(skip(self), fields(node_id = %self.node_id, severity))]
    pub async fn report_ddos(&self, severity: &str, description: &str, pps: f64) -> Result<()> {
        let url = format!("{}/api/v1/nodes/{}/ddos", self.base_url, self.node_id);
        debug!(%url, %severity, "Reporting DDoS event");

        let body = serde_json::json!({
            "severity": severity,
            "description": description,
            "pps": pps,
        });

        let resp = self
            .http
            .post(&url)
            .header("Authorization", self.auth_header())
            .json(&body)
            .send()
            .await
            .context("HTTP request failed for report_ddos")?;

        let status = resp.status();
        if !status.is_success() {
            let text = resp.text().await.unwrap_or_default();
            warn!(%status, response_body = %text, "report_ddos HTTP error (non-fatal)");
        }

        Ok(())
    }

    /// Report a player join/leave event parsed from the server's log stream.
    /// action is "join" or "leave". Fire-and-forget; non-2xx is logged.
    pub async fn report_player_event(
        &self,
        server_id: &str,
        action: &str,
        username: &str,
        hytale_uuid: &str,
    ) -> Result<()> {
        let url = format!(
            "{}/api/v1/servers/{}/daemon/players",
            self.base_url, server_id
        );
        let body = serde_json::json!({
            "action": action,
            "username": username,
            "hytale_uuid": hytale_uuid,
        });
        let resp = self
            .http
            .post(&url)
            .header("Authorization", self.auth_header())
            .json(&body)
            .send()
            .await
            .context("HTTP request failed for report_player_event")?;
        let status = resp.status();
        if !status.is_success() {
            let text = resp.text().await.unwrap_or_default();
            warn!(%status, response_body = %text, "report_player_event HTTP error (non-fatal)");
        }
        Ok(())
    }

    /// Fetch a fresh Hytale game session for a server about to start. Returns
    /// `None` when the panel has no Hytale account linked (204); the server
    /// then starts unauthenticated, as before.
    pub async fn fetch_game_session(&self, server_id: &str) -> Result<Option<GameSession>> {
        let url = format!(
            "{}/api/v1/servers/{}/daemon/game-session",
            self.base_url, server_id
        );
        let resp = self
            .http
            .post(&url)
            .header("Authorization", self.auth_header())
            .send()
            .await
            .context("HTTP request failed for fetch_game_session")?;
        let status = resp.status();
        if status == reqwest::StatusCode::NO_CONTENT {
            return Ok(None);
        }
        if !status.is_success() {
            let text = resp.text().await.unwrap_or_default();
            bail!("game session request failed: {status}: {text}");
        }
        Ok(Some(resp.json().await.context("decoding game session")?))
    }

    /// Report the worlds discovered on disk plus the active world (config.json).
    pub async fn report_worlds(
        &self,
        server_id: &str,
        worlds: &[ScannedWorld],
        active_world: &str,
    ) -> Result<()> {
        let url = format!(
            "{}/api/v1/servers/{}/daemon/worlds",
            self.base_url, server_id
        );
        let body = serde_json::json!({
            "worlds": worlds,
            "active_world": active_world,
        });
        let resp = self
            .http
            .post(&url)
            .header("Authorization", self.auth_header())
            .json(&body)
            .send()
            .await
            .context("HTTP request failed for report_worlds")?;
        let status = resp.status();
        if !status.is_success() {
            let text = resp.text().await.unwrap_or_default();
            warn!(%status, response_body = %text, "report_worlds HTTP error (non-fatal)");
        }
        Ok(())
    }

    /// Report detected plugins for a server to the TalePanel API.
    #[instrument(skip(self, plugins), fields(server_id, plugin_count = plugins.len()))]
    pub async fn report_plugins(
        &self,
        server_id: &str,
        plugins: &[crate::plugin_scanner::DetectedPlugin],
    ) -> Result<()> {
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

/// Tokens that let a Hytale server start authenticated (Hytale "token
/// passthrough"). Debug output is redacted so tokens never reach the logs.
#[derive(Clone, Deserialize)]
pub struct GameSession {
    pub session_token: String,
    pub identity_token: String,
    pub owner_uuid: String,
}

impl std::fmt::Debug for GameSession {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("GameSession")
            .field("session_token", &"<redacted>")
            .field("identity_token", &"<redacted>")
            .field("owner_uuid", &self.owner_uuid)
            .finish()
    }
}

#[cfg(test)]
mod heartbeat_body_tests {
    use super::*;

    fn metrics(mtls: Option<MtlsAdvert>) -> HeartbeatMetrics {
        HeartbeatMetrics {
            cpu_percent: 1.0,
            ram_used_mb: 2,
            disk_used_mb: 3,
            server_count: 0,
            timestamp: Utc::now(),
            mtls,
            servers: Vec::new(),
            isolation: None,
        }
    }

    #[test]
    fn heartbeat_body_carries_server_samples_only_when_present() {
        let m = metrics(None);
        let v = serde_json::to_value(HeartbeatBody::new("online", &m, "tok")).unwrap();
        assert!(!v.as_object().unwrap().contains_key("servers"));

        let mut m = metrics(None);
        m.servers.push(ServerSample {
            server_id: "abc".into(),
            cpu_percent: 12.5,
            ram_mb: 2048,
        });
        let v = serde_json::to_value(HeartbeatBody::new("online", &m, "tok")).unwrap();
        assert_eq!(
            v["servers"],
            serde_json::json!([{"server_id": "abc", "cpu_percent": 12.5, "ram_mb": 2048}])
        );
    }

    #[test]
    fn heartbeat_body_includes_mtls_fields() {
        let not_after = Utc::now();
        let m = metrics(Some(MtlsAdvert {
            serial: "1a2b".into(),
            not_after,
        }));
        let v = serde_json::to_value(HeartbeatBody::new("online", &m, "tok")).unwrap();
        assert_eq!(v["mtls_ready"], true);
        assert_eq!(v["cert_serial"], "1a2b");
        assert_eq!(
            v["cert_not_after"],
            serde_json::to_value(not_after).unwrap()
        );
        assert_eq!(v["status"], "online");
        assert_eq!(v["server_count"], 0);

        let m = metrics(None);
        let v = serde_json::to_value(HeartbeatBody::new("online", &m, "tok")).unwrap();
        let o = v.as_object().unwrap();
        assert!(!o.contains_key("mtls_ready"));
        assert!(!o.contains_key("cert_serial"));
        assert!(!o.contains_key("cert_not_after"));
        assert!(!o.contains_key("cert_renewal"));
        assert_eq!(v["node_token"], "tok");
    }

    #[test]
    fn heartbeat_body_announces_cert_renewal_with_mtls() {
        let m = metrics(Some(MtlsAdvert {
            serial: "1".into(),
            not_after: Utc::now(),
        }));
        let v = serde_json::to_value(HeartbeatBody::new("online", &m, "tok")).unwrap();
        assert_eq!(v["cert_renewal"], true);
    }

    #[test]
    fn heartbeat_hint_parsing() {
        assert_eq!(parse_heartbeat_hint(br#"{"message":"ok"}"#), None);
        assert_eq!(parse_heartbeat_hint(b"not json"), None);
        let h = parse_heartbeat_hint(
            br#"{"message":"ok","pki":{"ca_bundle_pem":"A","endorsements_pem":"E","renew":true}}"#,
        )
        .unwrap();
        assert_eq!(
            h,
            PkiHint {
                ca_bundle_pem: "A".into(),
                endorsements_pem: "E".into(),
                renew: true
            }
        );
        let h = parse_heartbeat_hint(br#"{"pki":{"ca_bundle_pem":"A","renew":false}}"#).unwrap();
        assert!(!h.renew && h.endorsements_pem.is_empty());
    }

    #[test]
    fn csr_response_without_endorsements_parses() {
        let r: NodeCertResponse =
            serde_json::from_str(r#"{"ca_cert_pem":"C","node_cert_pem":"N"}"#).unwrap();
        assert_eq!((r.ca_pem.as_str(), r.cert_pem.as_str()), ("C", "N"));
        assert!(r.endorsements_pem.is_empty());
    }
}
