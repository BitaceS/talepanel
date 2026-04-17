use std::path::PathBuf;
use std::sync::Arc;

use axum::{
    extract::{Multipart, Path, Query, State},
    http::StatusCode,
    response::{IntoResponse, Json},
    routing::{get, post, put},
    Router,
};
use serde::{Deserialize, Serialize};
use tokio::net::TcpListener;
use tracing::{info, instrument, warn};

use crate::net_monitor::NetMonitor;
use crate::process::{hytale::ServerConfig, ProcessManager, ServerStatus};

// ---------------------------------------------------------------------------
// Shared state
// ---------------------------------------------------------------------------

/// Application state shared across all route handlers.
#[derive(Clone)]
pub struct AppState {
    pub process_manager: Arc<ProcessManager>,
    pub node_id: String,
    /// The node's registration token — used to authenticate incoming API requests.
    pub node_token: String,
    /// Base directory containing all server data directories, e.g. `/srv/taledaemon/servers`.
    /// Each server's files live at `{servers_base_dir}/{server_id}`.
    pub servers_base_dir: String,
    /// Network traffic monitor for DDoS detection.
    pub net_monitor: NetMonitor,
}

// ---------------------------------------------------------------------------
// Response types
// ---------------------------------------------------------------------------

#[derive(Serialize)]
struct HealthResponse {
    status: &'static str,
    version: &'static str,
    node_id: String,
}

#[derive(Serialize)]
struct ServerEntry {
    server_id: String,
    status: String,
}

#[derive(Serialize)]
struct ServersResponse {
    servers: Vec<ServerEntry>,
    count: usize,
}

#[derive(Serialize)]
struct ProcessMetricsEntry {
    server_id: String,
    status: String,
    cpu_percent: f32,
    ram_mb: u64,
    uptime_s: u64,
}

#[derive(Serialize)]
struct MetricsResponse {
    server_count: usize,
    processes: Vec<ProcessMetricsEntry>,
}

// ---------------------------------------------------------------------------
// Route handlers
// ---------------------------------------------------------------------------

/// `GET /health`
///
/// Returns basic liveness information.  Used by load-balancers, container
/// orchestrators, and the TalePanel API to determine whether the daemon is up.
async fn health(State(state): State<AppState>) -> impl IntoResponse {
    Json(HealthResponse {
        status: "ok",
        version: env!("CARGO_PKG_VERSION"),
        node_id: state.node_id.clone(),
    })
}

/// `GET /servers`
///
/// Returns a JSON array of all server processes currently registered with the
/// ProcessManager, along with their current lifecycle status.
async fn list_servers(State(state): State<AppState>) -> impl IntoResponse {
    let server_ids = state.process_manager.list_servers();
    let mut entries: Vec<ServerEntry> = Vec::with_capacity(server_ids.len());

    for server_id in server_ids {
        // We need to reach into the process manager; expose a helper method
        // for status lookup without holding DashMap guards across await points.
        let status = state
            .process_manager
            .get_server_status(&server_id)
            .unwrap_or(ServerStatus::Stopped);

        entries.push(ServerEntry {
            server_id,
            status: status.to_string(),
        });
    }

    let count = entries.len();
    Json(ServersResponse {
        servers: entries,
        count,
    })
}

/// `GET /metrics`
///
/// Returns aggregated resource metrics for every managed server process.
/// Suitable for scraping by a lightweight monitoring sidecar or the TalePanel
/// dashboard.
async fn metrics(State(state): State<AppState>) -> impl IntoResponse {
    let all_metrics = state.process_manager.collect_all_metrics().await;

    let server_count = all_metrics.len();
    let mut processes: Vec<ProcessMetricsEntry> = Vec::with_capacity(server_count);

    for (server_id, m) in all_metrics {
        let status = state
            .process_manager
            .get_server_status(&server_id)
            .unwrap_or(ServerStatus::Running);

        processes.push(ProcessMetricsEntry {
            server_id,
            status: status.to_string(),
            cpu_percent: m.cpu_percent,
            ram_mb: m.ram_mb,
            uptime_s: m.uptime_s,
        });
    }

    // Sort by server_id for stable output.
    processes.sort_by(|a, b| a.server_id.cmp(&b.server_id));

    Json(MetricsResponse {
        server_count,
        processes,
    })
}

// ─────────────────────────────────────────────────────────────────────────────
// Power action request/response types
// ─────────────────────────────────────────────────────────────────────────────

#[derive(Deserialize)]
struct StartRequest {
    config: ServerConfig,
}

#[derive(Deserialize)]
struct CommandRequest {
    command: String,
}

#[derive(Deserialize)]
struct ProvisionRequest {
    version: String,
    data_path: String,
}

#[derive(Serialize)]
struct ActionResponse {
    success: bool,
    message: String,
}

impl ActionResponse {
    fn ok(msg: impl Into<String>) -> Json<Self> {
        Json(Self { success: true, message: msg.into() })
    }
    fn err(msg: impl Into<String>) -> (StatusCode, Json<Self>) {
        (
            StatusCode::BAD_REQUEST,
            Json(Self { success: false, message: msg.into() }),
        )
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Power action handlers
// ─────────────────────────────────────────────────────────────────────────────

/// `POST /servers/:id/start`
/// Body: {"config": <ServerConfig>}
async fn start_server(
    State(state): State<AppState>,
    Path(server_id): Path<String>,
    Json(req): Json<StartRequest>,
) -> impl IntoResponse {
    let mut cfg = req.config;
    cfg.server_id = server_id.clone(); // ensure ID matches path

    match state.process_manager.start_server(cfg).await {
        Ok(_) => ActionResponse::ok(format!("Server {server_id} is starting")).into_response(),
        Err(e) => ActionResponse::err(e.to_string()).into_response(),
    }
}

/// `POST /servers/:id/stop`
async fn stop_server(
    State(state): State<AppState>,
    Path(server_id): Path<String>,
) -> impl IntoResponse {
    match state.process_manager.stop_server(&server_id).await {
        Ok(_) => ActionResponse::ok(format!("Server {server_id} stopped")).into_response(),
        Err(e) => ActionResponse::err(e.to_string()).into_response(),
    }
}

/// `POST /servers/:id/restart`
async fn restart_server(
    State(state): State<AppState>,
    Path(server_id): Path<String>,
) -> impl IntoResponse {
    match state.process_manager.restart_server(&server_id).await {
        Ok(_) => ActionResponse::ok(format!("Server {server_id} restarting")).into_response(),
        Err(e) => ActionResponse::err(e.to_string()).into_response(),
    }
}

/// `POST /servers/:id/kill`
/// DELETE /servers/:id
/// Kills the running process (if any) and removes the server's data directory
/// from disk.  Called by the API immediately before it deletes the server row.
async fn delete_server_data(
    State(state): State<AppState>,
    Path(server_id): Path<String>,
) -> impl IntoResponse {
    // Best-effort kill — ignore "not running" errors.
    let _ = state.process_manager.kill_server(&server_id).await;

    let data_path = std::path::PathBuf::from(&state.config.daemon.data_root)
        .join("servers")
        .join(&server_id);

    if data_path.exists() {
        if let Err(e) = tokio::fs::remove_dir_all(&data_path).await {
            return ActionResponse::err(format!(
                "failed to remove {}: {}",
                data_path.display(),
                e
            ))
            .into_response();
        }
    }
    ActionResponse::ok(format!("server {server_id} data removed")).into_response()
}

async fn kill_server(
    State(state): State<AppState>,
    Path(server_id): Path<String>,
) -> impl IntoResponse {
    match state.process_manager.kill_server(&server_id).await {
        Ok(_) => ActionResponse::ok(format!("Server {server_id} killed")).into_response(),
        Err(e) => ActionResponse::err(e.to_string()).into_response(),
    }
}

/// `POST /servers/:id/provision`
/// Body: {"version": "latest", "data_path": "/srv/taledaemon/servers/<id>"}
///
/// Spawns a background task that downloads Hytale server files via the
/// Hytale Downloader CLI.  Returns immediately; status is reported back to the
/// TalePanel API when the download completes.
async fn provision_server(
    State(state): State<AppState>,
    Path(server_id): Path<String>,
    Json(req): Json<ProvisionRequest>,
) -> impl IntoResponse {
    match state
        .process_manager
        .provision_server(server_id.clone(), req.version, req.data_path)
        .await
    {
        Ok(_) => ActionResponse::ok(format!(
            "Server {server_id} provisioning started in background"
        ))
        .into_response(),
        Err(e) => ActionResponse::err(e.to_string()).into_response(),
    }
}

/// `POST /servers/:id/command`
/// Body: {"command": "say Hello world"}
async fn send_command(
    State(state): State<AppState>,
    Path(server_id): Path<String>,
    Json(req): Json<CommandRequest>,
) -> impl IntoResponse {
    match state.process_manager.send_command(&server_id, &req.command).await {
        Ok(_) => ActionResponse::ok("Command sent").into_response(),
        Err(e) => ActionResponse::err(e.to_string()).into_response(),
    }
}

/// `GET /servers/:id/status`
async fn server_status(
    State(state): State<AppState>,
    Path(server_id): Path<String>,
) -> impl IntoResponse {
    match state.process_manager.get_server_status(&server_id) {
        Some(status) => Json(serde_json::json!({
            "server_id": server_id,
            "status": status.to_string(),
        }))
        .into_response(),
        None => (
            StatusCode::NOT_FOUND,
            Json(serde_json::json!({"error": "server not found on this node"})),
        )
            .into_response(),
    }
}

/// `GET /servers/:id/metrics`
async fn server_metrics(
    State(state): State<AppState>,
    Path(server_id): Path<String>,
) -> impl IntoResponse {
    // Collect all metrics and return the entry for this server
    let all = state.process_manager.collect_all_metrics().await;
    match all.get(&server_id) {
        Some(m) => Json(serde_json::json!({
            "server_id": server_id,
            "cpu_percent": m.cpu_percent,
            "ram_mb": m.ram_mb,
            "uptime_s": m.uptime_s,
        }))
        .into_response(),
        None => (
            StatusCode::NOT_FOUND,
            Json(serde_json::json!({"error": "server not found on this node"})),
        )
            .into_response(),
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// File browser types
// ─────────────────────────────────────────────────────────────────────────────

/// Maximum file size that can be read or written via the file browser (1 MB).
const FILE_SIZE_LIMIT: u64 = 1_048_576;

#[derive(Deserialize)]
struct FileQuery {
    path: Option<String>,
}

#[derive(Serialize)]
struct FileEntry {
    name: String,
    size: u64,
    is_dir: bool,
    modified: String,
}

#[derive(Serialize)]
struct ListFilesResponse {
    entries: Vec<FileEntry>,
    path: String,
}

#[derive(Serialize)]
struct FileContentResponse {
    content: String,
}

#[derive(Deserialize)]
struct WriteFileRequest {
    path: String,
    content: String,
}

#[derive(Deserialize)]
struct CreateDirRequest {
    path: String,
}

#[derive(Deserialize)]
struct RenameRequest {
    path: String,
    new_name: String,
}

#[derive(Deserialize)]
struct ExtractRequest {
    path: String,
}

#[derive(Deserialize)]
struct ArchiveRequest {
    path: String,
}

// ─────────────────────────────────────────────────────────────────────────────
// File browser path security
// ─────────────────────────────────────────────────────────────────────────────

/// Resolve a relative `user_path` within the server's base directory, ensuring
/// the result does not escape the base via `..` or symlinks.
///
/// Returns the canonicalized absolute path on success, or an error response.
async fn resolve_safe_path(
    servers_base_dir: &str,
    server_id: &str,
    user_path: &str,
) -> Result<PathBuf, (StatusCode, Json<serde_json::Value>)> {
    let base = PathBuf::from(servers_base_dir).join(server_id);

    // Ensure the base directory exists — create it automatically so the file
    // manager works even before the server has been provisioned/started.
    if !base.exists() {
        if let Err(e) = tokio::fs::create_dir_all(&base).await {
            warn!(%e, "failed to create server directory");
            return Err((
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({"error": format!("failed to create server directory: {e}")})),
            ));
        }
        info!(path = %base.display(), "Created server directory on first access");
    }

    // Canonicalize the base directory so we have a reliable prefix to compare.
    let canonical_base = base.canonicalize().map_err(|e| {
        warn!(%e, "failed to canonicalize server base dir");
        (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(serde_json::json!({"error": "failed to resolve server directory"})),
        )
    })?;

    // Strip leading slash from user path so it is always relative.
    let relative = user_path.trim_start_matches('/');

    // Reject any path component that is exactly ".."
    for component in std::path::Path::new(relative).components() {
        if let std::path::Component::ParentDir = component {
            return Err((
                StatusCode::FORBIDDEN,
                Json(serde_json::json!({"error": "path traversal is not allowed"})),
            ));
        }
    }

    let target = canonical_base.join(relative);

    // If the target already exists we can canonicalize it for a definitive check.
    // If it doesn't exist (e.g. we're about to create it) we canonicalize the
    // parent and verify that it is still inside the base.
    let resolved = if target.exists() {
        target.canonicalize().map_err(|e| {
            warn!(%e, "failed to canonicalize target path");
            (
                StatusCode::BAD_REQUEST,
                Json(serde_json::json!({"error": "invalid path"})),
            )
        })?
    } else {
        // The target doesn't exist yet; canonicalize its parent directory.
        let parent = target.parent().ok_or_else(|| {
            (
                StatusCode::BAD_REQUEST,
                Json(serde_json::json!({"error": "invalid path"})),
            )
        })?;
        if !parent.exists() {
            return Err((
                StatusCode::NOT_FOUND,
                Json(serde_json::json!({"error": "parent directory does not exist"})),
            ));
        }
        let canonical_parent = parent.canonicalize().map_err(|e| {
            warn!(%e, "failed to canonicalize parent path");
            (
                StatusCode::BAD_REQUEST,
                Json(serde_json::json!({"error": "invalid path"})),
            )
        })?;
        if !canonical_parent.starts_with(&canonical_base) {
            return Err((
                StatusCode::FORBIDDEN,
                Json(serde_json::json!({"error": "path traversal is not allowed"})),
            ));
        }
        // Reconstruct the full path using the canonical parent + file name.
        let file_name = target.file_name().ok_or_else(|| {
            (
                StatusCode::BAD_REQUEST,
                Json(serde_json::json!({"error": "invalid path"})),
            )
        })?;
        canonical_parent.join(file_name)
    };

    if !resolved.starts_with(&canonical_base) {
        return Err((
            StatusCode::FORBIDDEN,
            Json(serde_json::json!({"error": "path traversal is not allowed"})),
        ));
    }

    Ok(resolved)
}

// ─────────────────────────────────────────────────────────────────────────────
// File browser handlers
// ─────────────────────────────────────────────────────────────────────────────

/// `GET /servers/:id/files?path=`
///
/// List directory contents. Returns a JSON array of file entries.
async fn list_files(
    State(state): State<AppState>,
    Path(server_id): Path<String>,
    Query(query): Query<FileQuery>,
) -> impl IntoResponse {
    let rel_path = query.path.unwrap_or_else(|| "/".to_string());

    let target = match resolve_safe_path(&state.servers_base_dir, &server_id, &rel_path).await {
        Ok(p) => p,
        Err(e) => return e.into_response(),
    };

    if !target.is_dir() {
        return (
            StatusCode::BAD_REQUEST,
            Json(serde_json::json!({"error": "path is not a directory"})),
        )
            .into_response();
    }

    let mut dir = match tokio::fs::read_dir(&target).await {
        Ok(d) => d,
        Err(e) => {
            return (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({"error": format!("failed to read directory: {e}")})),
            )
                .into_response();
        }
    };

    let mut entries = Vec::new();
    while let Ok(Some(entry)) = dir.next_entry().await {
        let metadata = match entry.metadata().await {
            Ok(m) => m,
            Err(_) => continue,
        };
        let modified = metadata
            .modified()
            .ok()
            .and_then(|t| {
                let dt: chrono::DateTime<chrono::Utc> = t.into();
                Some(dt.to_rfc3339())
            })
            .unwrap_or_default();

        entries.push(FileEntry {
            name: entry.file_name().to_string_lossy().to_string(),
            size: metadata.len(),
            is_dir: metadata.is_dir(),
            modified,
        });
    }

    // Sort: directories first, then alphabetically by name.
    entries.sort_by(|a, b| {
        b.is_dir
            .cmp(&a.is_dir)
            .then_with(|| a.name.to_lowercase().cmp(&b.name.to_lowercase()))
    });

    Json(ListFilesResponse {
        entries,
        path: rel_path,
    })
    .into_response()
}

/// `GET /servers/:id/files/content?path=`
///
/// Read a text file (max 1 MB). Returns `{"content": "..."}`.
async fn read_file(
    State(state): State<AppState>,
    Path(server_id): Path<String>,
    Query(query): Query<FileQuery>,
) -> impl IntoResponse {
    let rel_path = match &query.path {
        Some(p) => p.as_str(),
        None => {
            return (
                StatusCode::BAD_REQUEST,
                Json(serde_json::json!({"error": "path query parameter is required"})),
            )
                .into_response();
        }
    };

    let target = match resolve_safe_path(&state.servers_base_dir, &server_id, rel_path).await {
        Ok(p) => p,
        Err(e) => return e.into_response(),
    };

    if !target.is_file() {
        return (
            StatusCode::NOT_FOUND,
            Json(serde_json::json!({"error": "file not found"})),
        )
            .into_response();
    }

    // Check file size before reading.
    let metadata = match tokio::fs::metadata(&target).await {
        Ok(m) => m,
        Err(e) => {
            return (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({"error": format!("failed to read file metadata: {e}")})),
            )
                .into_response();
        }
    };

    if metadata.len() > FILE_SIZE_LIMIT {
        return (
            StatusCode::BAD_REQUEST,
            Json(serde_json::json!({"error": format!("file exceeds maximum size of {} bytes", FILE_SIZE_LIMIT)})),
        )
            .into_response();
    }

    match tokio::fs::read_to_string(&target).await {
        Ok(content) => Json(FileContentResponse { content }).into_response(),
        Err(e) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(serde_json::json!({"error": format!("failed to read file: {e}")})),
        )
            .into_response(),
    }
}

/// `PUT /servers/:id/files/content`
///
/// Write a text file. Body: `{"path": "...", "content": "..."}`. Max 1 MB.
async fn write_file(
    State(state): State<AppState>,
    Path(server_id): Path<String>,
    Json(req): Json<WriteFileRequest>,
) -> impl IntoResponse {
    if req.content.len() as u64 > FILE_SIZE_LIMIT {
        return (
            StatusCode::BAD_REQUEST,
            Json(serde_json::json!({"error": format!("content exceeds maximum size of {} bytes", FILE_SIZE_LIMIT)})),
        )
            .into_response();
    }

    let target = match resolve_safe_path(&state.servers_base_dir, &server_id, &req.path).await {
        Ok(p) => p,
        Err(e) => return e.into_response(),
    };

    // Refuse to overwrite a directory.
    if target.is_dir() {
        return (
            StatusCode::BAD_REQUEST,
            Json(serde_json::json!({"error": "path is a directory, not a file"})),
        )
            .into_response();
    }

    match tokio::fs::write(&target, &req.content).await {
        Ok(_) => ActionResponse::ok("file written successfully").into_response(),
        Err(e) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(serde_json::json!({"error": format!("failed to write file: {e}")})),
        )
            .into_response(),
    }
}

/// `DELETE /servers/:id/files?path=`
///
/// Delete a file or empty directory.
async fn delete_file(
    State(state): State<AppState>,
    Path(server_id): Path<String>,
    Query(query): Query<FileQuery>,
) -> impl IntoResponse {
    let rel_path = match &query.path {
        Some(p) => p.as_str(),
        None => {
            return (
                StatusCode::BAD_REQUEST,
                Json(serde_json::json!({"error": "path query parameter is required"})),
            )
                .into_response();
        }
    };

    // Prevent deleting the server root directory itself.
    let trimmed = rel_path.trim_matches('/');
    if trimmed.is_empty() {
        return (
            StatusCode::FORBIDDEN,
            Json(serde_json::json!({"error": "cannot delete the server root directory"})),
        )
            .into_response();
    }

    let target = match resolve_safe_path(&state.servers_base_dir, &server_id, rel_path).await {
        Ok(p) => p,
        Err(e) => return e.into_response(),
    };

    if !target.exists() {
        return (
            StatusCode::NOT_FOUND,
            Json(serde_json::json!({"error": "file or directory not found"})),
        )
            .into_response();
    }

    let result = if target.is_dir() {
        tokio::fs::remove_dir(&target).await
    } else {
        tokio::fs::remove_file(&target).await
    };

    match result {
        Ok(_) => ActionResponse::ok("deleted successfully").into_response(),
        Err(e) => {
            let msg = if target.is_dir() {
                format!("failed to delete directory (it may not be empty): {e}")
            } else {
                format!("failed to delete file: {e}")
            };
            (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({"error": msg})),
            )
                .into_response()
        }
    }
}

/// `POST /servers/:id/files/directory`
///
/// Create a directory. Body: `{"path": "..."}`.
async fn create_directory(
    State(state): State<AppState>,
    Path(server_id): Path<String>,
    Json(req): Json<CreateDirRequest>,
) -> impl IntoResponse {
    let target = match resolve_safe_path(&state.servers_base_dir, &server_id, &req.path).await {
        Ok(p) => p,
        Err(e) => return e.into_response(),
    };

    if target.exists() {
        return (
            StatusCode::BAD_REQUEST,
            Json(serde_json::json!({"error": "path already exists"})),
        )
            .into_response();
    }

    match tokio::fs::create_dir(&target).await {
        Ok(_) => ActionResponse::ok("directory created").into_response(),
        Err(e) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(serde_json::json!({"error": format!("failed to create directory: {e}")})),
        )
            .into_response(),
    }
}

/// `POST /servers/:id/files/rename`
///
/// Rename a file or directory. Body: `{"path": "...", "new_name": "..."}`.
/// `new_name` is just the new file/directory name, NOT a full path.
async fn rename_file(
    State(state): State<AppState>,
    Path(server_id): Path<String>,
    Json(req): Json<RenameRequest>,
) -> impl IntoResponse {
    // Validate new_name: must not contain path separators or be empty.
    if req.new_name.is_empty()
        || req.new_name.contains('/')
        || req.new_name.contains('\\')
        || req.new_name == "."
        || req.new_name == ".."
    {
        return (
            StatusCode::BAD_REQUEST,
            Json(serde_json::json!({"error": "invalid new_name: must be a simple file or directory name"})),
        )
            .into_response();
    }

    let source = match resolve_safe_path(&state.servers_base_dir, &server_id, &req.path).await {
        Ok(p) => p,
        Err(e) => return e.into_response(),
    };

    if !source.exists() {
        return (
            StatusCode::NOT_FOUND,
            Json(serde_json::json!({"error": "source file or directory not found"})),
        )
            .into_response();
    }

    let dest = match source.parent() {
        Some(parent) => parent.join(&req.new_name),
        None => {
            return (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({"error": "cannot determine parent directory"})),
            )
                .into_response();
        }
    };

    if dest.exists() {
        return (
            StatusCode::BAD_REQUEST,
            Json(serde_json::json!({"error": "a file or directory with that name already exists"})),
        )
            .into_response();
    }

    match tokio::fs::rename(&source, &dest).await {
        Ok(_) => ActionResponse::ok("renamed successfully").into_response(),
        Err(e) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(serde_json::json!({"error": format!("failed to rename: {e}")})),
        )
            .into_response(),
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// File upload / download / extract / archive handlers
// ─────────────────────────────────────────────────────────────────────────────

/// `POST /servers/:id/files/upload?path=`
///
/// Accept a multipart file upload and save it to the specified directory.
async fn upload_file(
    State(state): State<AppState>,
    Path(server_id): Path<String>,
    Query(query): Query<FileQuery>,
    mut multipart: Multipart,
) -> impl IntoResponse {
    let rel_dir = query.path.unwrap_or_else(|| "/".to_string());

    let target_dir = match resolve_safe_path(&state.servers_base_dir, &server_id, &rel_dir).await {
        Ok(p) => p,
        Err(e) => return e.into_response(),
    };

    if !target_dir.is_dir() {
        return (
            StatusCode::BAD_REQUEST,
            Json(serde_json::json!({"error": "target path is not a directory"})),
        )
            .into_response();
    }

    // 50 MB upload limit
    const MAX_UPLOAD: usize = 50 * 1024 * 1024;
    let mut saved = 0u32;

    while let Ok(Some(field)) = multipart.next_field().await {
        let file_name = match field.file_name() {
            Some(name) => name.to_string(),
            None => continue,
        };

        // Validate filename — no path separators
        if file_name.contains('/') || file_name.contains('\\') || file_name == ".." || file_name == "." {
            return (
                StatusCode::BAD_REQUEST,
                Json(serde_json::json!({"error": format!("invalid filename: {file_name}")})),
            )
                .into_response();
        }

        let data = match field.bytes().await {
            Ok(d) => d,
            Err(e) => {
                return (
                    StatusCode::BAD_REQUEST,
                    Json(serde_json::json!({"error": format!("failed to read upload: {e}")})),
                )
                    .into_response();
            }
        };

        if data.len() > MAX_UPLOAD {
            return (
                StatusCode::BAD_REQUEST,
                Json(serde_json::json!({"error": format!("file exceeds maximum upload size of {} MB", MAX_UPLOAD / 1024 / 1024)})),
            )
                .into_response();
        }

        let dest = target_dir.join(&file_name);
        if let Err(e) = tokio::fs::write(&dest, &data).await {
            return (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({"error": format!("failed to save file: {e}")})),
            )
                .into_response();
        }

        saved += 1;
    }

    ActionResponse::ok(format!("{saved} file(s) uploaded")).into_response()
}

/// `GET /servers/:id/files/download?path=`
///
/// Download a file as a binary stream.
async fn download_file(
    State(state): State<AppState>,
    Path(server_id): Path<String>,
    Query(query): Query<FileQuery>,
) -> impl IntoResponse {
    let rel_path = match &query.path {
        Some(p) => p.as_str(),
        None => {
            return (
                StatusCode::BAD_REQUEST,
                Json(serde_json::json!({"error": "path query parameter is required"})),
            )
                .into_response();
        }
    };

    let target = match resolve_safe_path(&state.servers_base_dir, &server_id, rel_path).await {
        Ok(p) => p,
        Err(e) => return e.into_response(),
    };

    if !target.is_file() {
        return (
            StatusCode::NOT_FOUND,
            Json(serde_json::json!({"error": "file not found"})),
        )
            .into_response();
    }

    let file_name = target
        .file_name()
        .map(|n| n.to_string_lossy().to_string())
        .unwrap_or_else(|| "download".to_string());

    match tokio::fs::read(&target).await {
        Ok(data) => {
            let headers = [
                (axum::http::header::CONTENT_TYPE, "application/octet-stream".to_string()),
                (
                    axum::http::header::CONTENT_DISPOSITION,
                    format!("attachment; filename=\"{file_name}\""),
                ),
            ];
            (headers, data).into_response()
        }
        Err(e) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(serde_json::json!({"error": format!("failed to read file: {e}")})),
        )
            .into_response(),
    }
}

/// `POST /servers/:id/files/extract`
///
/// Extract a zip archive in place (creates a directory next to the archive).
/// Body: `{"path": "path/to/archive.zip"}`
async fn extract_archive(
    State(state): State<AppState>,
    Path(server_id): Path<String>,
    Json(req): Json<ExtractRequest>,
) -> impl IntoResponse {
    let target = match resolve_safe_path(&state.servers_base_dir, &server_id, &req.path).await {
        Ok(p) => p,
        Err(e) => return e.into_response(),
    };

    if !target.is_file() {
        return (
            StatusCode::NOT_FOUND,
            Json(serde_json::json!({"error": "archive file not found"})),
        )
            .into_response();
    }

    let ext = target
        .extension()
        .map(|e| e.to_string_lossy().to_lowercase())
        .unwrap_or_default();

    if ext != "zip" {
        return (
            StatusCode::BAD_REQUEST,
            Json(serde_json::json!({"error": "only .zip archives are supported"})),
        )
            .into_response();
    }

    // Extract into the same directory as the archive
    let extract_dir = target.parent().unwrap().to_path_buf();
    let archive_path = target.clone();

    // Run extraction in a blocking task
    let result = tokio::task::spawn_blocking(move || -> Result<usize, String> {
        let file = std::fs::File::open(&archive_path).map_err(|e| format!("open archive: {e}"))?;
        let mut archive = zip::ZipArchive::new(file).map_err(|e| format!("read archive: {e}"))?;
        let count = archive.len();

        for i in 0..count {
            let mut entry = archive.by_index(i).map_err(|e| format!("read entry: {e}"))?;
            let name = entry.name().to_string();

            // Security: reject entries with path traversal
            if name.contains("..") {
                continue;
            }

            let out_path = extract_dir.join(&name);

            if entry.is_dir() {
                std::fs::create_dir_all(&out_path).map_err(|e| format!("mkdir: {e}"))?;
            } else {
                if let Some(parent) = out_path.parent() {
                    std::fs::create_dir_all(parent).map_err(|e| format!("mkdir parent: {e}"))?;
                }
                let mut out_file = std::fs::File::create(&out_path).map_err(|e| format!("create file: {e}"))?;
                std::io::copy(&mut entry, &mut out_file).map_err(|e| format!("write file: {e}"))?;
            }
        }

        Ok(count)
    })
    .await;

    match result {
        Ok(Ok(count)) => ActionResponse::ok(format!("extracted {count} entries")).into_response(),
        Ok(Err(e)) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(serde_json::json!({"error": e})),
        )
            .into_response(),
        Err(e) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(serde_json::json!({"error": format!("extraction task failed: {e}")})),
        )
            .into_response(),
    }
}

/// `POST /servers/:id/files/archive`
///
/// Create a zip archive from a file or directory.
/// Body: `{"path": "path/to/dir_or_file"}`
/// Output: `{path}.zip` in the same parent directory.
async fn create_archive(
    State(state): State<AppState>,
    Path(server_id): Path<String>,
    Json(req): Json<ArchiveRequest>,
) -> impl IntoResponse {
    let target = match resolve_safe_path(&state.servers_base_dir, &server_id, &req.path).await {
        Ok(p) => p,
        Err(e) => return e.into_response(),
    };

    if !target.exists() {
        return (
            StatusCode::NOT_FOUND,
            Json(serde_json::json!({"error": "path not found"})),
        )
            .into_response();
    }

    let archive_name = format!(
        "{}.zip",
        target.file_name().unwrap_or_default().to_string_lossy()
    );
    let archive_path = target.parent().unwrap().join(&archive_name);
    let source = target.clone();

    let result = tokio::task::spawn_blocking(move || -> Result<String, String> {
        let file = std::fs::File::create(&archive_path).map_err(|e| format!("create archive: {e}"))?;
        let mut zip_writer = zip::ZipWriter::new(file);
        let options = zip::write::SimpleFileOptions::default()
            .compression_method(zip::CompressionMethod::Deflated);

        if source.is_file() {
            let name = source.file_name().unwrap().to_string_lossy().to_string();
            zip_writer.start_file(&name, options).map_err(|e| format!("start file: {e}"))?;
            let data = std::fs::read(&source).map_err(|e| format!("read file: {e}"))?;
            std::io::Write::write_all(&mut zip_writer, &data).map_err(|e| format!("write: {e}"))?;
        } else {
            // Recursively add directory contents
            fn add_dir(
                writer: &mut zip::ZipWriter<std::fs::File>,
                base: &std::path::Path,
                current: &std::path::Path,
                options: zip::write::SimpleFileOptions,
            ) -> Result<(), String> {
                for entry in std::fs::read_dir(current).map_err(|e| format!("read dir: {e}"))? {
                    let entry = entry.map_err(|e| format!("dir entry: {e}"))?;
                    let path = entry.path();
                    let rel = path.strip_prefix(base).unwrap().to_string_lossy().replace('\\', "/");

                    if path.is_dir() {
                        writer.add_directory(&format!("{rel}/"), options).map_err(|e| format!("add dir: {e}"))?;
                        add_dir(writer, base, &path, options)?;
                    } else {
                        writer.start_file(&rel, options).map_err(|e| format!("start file: {e}"))?;
                        let data = std::fs::read(&path).map_err(|e| format!("read: {e}"))?;
                        std::io::Write::write_all(writer, &data).map_err(|e| format!("write: {e}"))?;
                    }
                }
                Ok(())
            }

            add_dir(&mut zip_writer, &source, &source, options)?;
        }

        zip_writer.finish().map_err(|e| format!("finish archive: {e}"))?;
        Ok(archive_name)
    })
    .await;

    match result {
        Ok(Ok(name)) => ActionResponse::ok(format!("created archive: {name}")).into_response(),
        Ok(Err(e)) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(serde_json::json!({"error": e})),
        )
            .into_response(),
        Err(e) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(serde_json::json!({"error": format!("archive task failed: {e}")})),
        )
            .into_response(),
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Network stats handler (delegates to NetMonitor)
// ─────────────────────────────────────────────────────────────────────────────

/// `GET /network-stats`
///
/// Returns comprehensive network traffic analysis including PPS/BPS rates,
/// threat detection, and mitigation status from the background NetMonitor.
async fn network_stats(State(state): State<AppState>) -> impl IntoResponse {
    let snapshot = state.net_monitor.snapshot().await;
    Json(snapshot)
}

// ─────────────────────────────────────────────────────────────────────────────
// Auth middleware — verify node token on all requests
// ─────────────────────────────────────────────────────────────────────────────

/// Simple bearer token extractor for daemon endpoint auth.
/// The Go API sends: Authorization: Bearer <node_token>
async fn auth_middleware(
    State(state): State<AppState>,
    req: axum::http::Request<axum::body::Body>,
    next: axum::middleware::Next,
) -> impl IntoResponse {
    let token = req
        .headers()
        .get(axum::http::header::AUTHORIZATION)
        .and_then(|v| v.to_str().ok())
        .and_then(|v| v.strip_prefix("Bearer "));

    match token {
        Some(t) if t == state.node_token => next.run(req).await.into_response(),
        _ => (
            StatusCode::UNAUTHORIZED,
            Json(serde_json::json!({"error": "unauthorized"})),
        )
            .into_response(),
    }
}

/// Catch-all 404 handler.
async fn not_found() -> impl IntoResponse {
    (StatusCode::NOT_FOUND, Json(serde_json::json!({"error": "not found"})))
}

// ─────────────────────────────────────────────────────────────────────────────
// Server bootstrap
// ─────────────────────────────────────────────────────────────────────────────

/// Build the Axum router with all local API routes.
pub fn build_router(state: AppState) -> Router {
    // Protected routes (require node token auth)
    let protected = Router::new()
        .route("/servers", get(list_servers))
        .route("/servers/:id/provision", post(provision_server))
        .route("/servers/:id/start", post(start_server))
        .route("/servers/:id/stop", post(stop_server))
        .route("/servers/:id/restart", post(restart_server))
        .route("/servers/:id/kill", post(kill_server))
        .route("/servers/:id", axum::routing::delete(delete_server_data))
        .route("/servers/:id/command", post(send_command))
        .route("/servers/:id/status", get(server_status))
        .route("/servers/:id/metrics", get(server_metrics))
        .route("/metrics", get(metrics))
        .route("/network-stats", get(network_stats))
        // File browser
        .route("/servers/:id/files", get(list_files).delete(delete_file))
        .route("/servers/:id/files/content", get(read_file).put(write_file))
        .route("/servers/:id/files/directory", post(create_directory))
        .route("/servers/:id/files/rename", post(rename_file))
        .route("/servers/:id/files/upload", post(upload_file))
        .route("/servers/:id/files/download", get(download_file))
        .route("/servers/:id/files/extract", post(extract_archive))
        .route("/servers/:id/files/archive", post(create_archive))
        .layer(axum::middleware::from_fn_with_state(state.clone(), auth_middleware));

    Router::new()
        // Health is public — used by load-balancers without auth
        .route("/health", get(health))
        .merge(protected)
        .fallback(not_found)
        .with_state(state)
}

/// Bind a TCP listener and serve the local HTTP API until cancelled.
///
/// This is an infinite future; cancel it (via `tokio::select!` or
/// `JoinHandle::abort()`) to shut down the server.
#[instrument(skip(state))]
pub async fn run_http_server(
    state: AppState,
    listen_addr: std::net::SocketAddr,
) -> anyhow::Result<()> {
    let router = build_router(state);

    let listener = TcpListener::bind(listen_addr)
        .await
        .map_err(|e| anyhow::anyhow!("Failed to bind HTTP server to {listen_addr}: {e}"))?;

    info!(%listen_addr, "Local HTTP server listening");

    axum::serve(listener, router)
        .await
        .map_err(|e| anyhow::anyhow!("HTTP server error: {e}"))?;

    Ok(())
}
