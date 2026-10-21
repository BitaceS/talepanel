use std::path::PathBuf;
use std::sync::Arc;

use axum::{
    extract::{Multipart, Path, Query, State},
    http::StatusCode,
    response::{IntoResponse, Json},
    routing::{get, post},
    Router,
};
use serde::{Deserialize, Serialize};
use tokio::io::AsyncWriteExt;
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
    /// Base directory for backup archives, e.g. `/srv/taledaemon/backups`. Kept
    /// outside `servers_base_dir` so archives are not themselves backed up.
    /// A backup lives at `{backups_base_dir}/{server_id}/{backup_id}.zip`.
    pub backups_base_dir: String,
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
    // The id becomes a path component handed to remove_dir_all.
    if !valid_server_id(&server_id) {
        return ActionResponse::err("invalid server id").into_response();
    }

    // Best-effort kill — ignore "not running" errors.
    let _ = state.process_manager.kill_server(&server_id).await;

    let data_path = std::path::PathBuf::from(&state.servers_base_dir)
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
// Filesystem safety primitives
// ─────────────────────────────────────────────────────────────────────────────

/// Filesystem primitives for writing into tenant-controlled directories.
///
/// Tenants can run code in, and create symlinks or hardlinks inside, their own
/// server directory. Every daemon write below such a directory therefore
/// (a) never follows a symlink, and (b) never writes through an existing inode
/// in place: data goes to a fresh temp file created with `create_new` in the
/// same directory, which is then renamed over the target. A rename replaces the
/// directory entry, so a planted symlink, or a hardlink to a file shared with
/// another server, is swapped out instead of being written through.
pub(crate) mod safe_fs {
    use std::fs::{self, File, OpenOptions};
    use std::io::{self, Read, Seek, Write};
    use std::path::{Component, Path, PathBuf};

    /// Upper bound on the bytes one archive extraction may write (20 GiB).
    pub const MAX_EXTRACT_BYTES: u64 = 20 * 1024 * 1024 * 1024;
    /// Upper bound on the number of entries in one extracted archive.
    pub const MAX_EXTRACT_ENTRIES: usize = 200_000;

    const TEMP_PREFIX: &str = ".tp-";
    const TEMP_SUFFIX: &str = ".tmp";

    /// Decompression-bomb limits for [`extract_zip`].
    #[derive(Clone, Copy, Debug)]
    pub struct ExtractLimits {
        pub max_total_bytes: u64,
        pub max_entries: usize,
    }

    impl Default for ExtractLimits {
        fn default() -> Self {
            Self { max_total_bytes: MAX_EXTRACT_BYTES, max_entries: MAX_EXTRACT_ENTRIES }
        }
    }

    #[derive(Debug, Default)]
    pub struct ExtractStats {
        /// Files written.
        pub written: usize,
        /// Entries skipped: unsafe names, symlinks, or paths crossing a symlink.
        pub skipped: usize,
    }

    /// True for the daemon's own in-flight temp files; archivers skip them.
    pub fn is_temp_name(name: &str) -> bool {
        name.starts_with(TEMP_PREFIX) && name.ends_with(TEMP_SUFFIX)
    }

    fn temp_path_in(dir: &Path) -> PathBuf {
        dir.join(format!("{TEMP_PREFIX}{}{TEMP_SUFFIX}", uuid::Uuid::new_v4().simple()))
    }

    /// True if `path` itself is a symlink (the link is not followed).
    pub fn is_symlink(path: &Path) -> bool {
        fs::symlink_metadata(path)
            .map(|m| m.file_type().is_symlink())
            .unwrap_or(false)
    }

    /// Open a file for reading, failing if the final component is a symlink.
    pub fn open_nofollow(path: &Path) -> io::Result<File> {
        let mut opts = OpenOptions::new();
        opts.read(true);
        #[cfg(unix)]
        {
            use std::os::unix::fs::OpenOptionsExt;
            opts.custom_flags(libc::O_NOFOLLOW);
        }
        opts.open(path)
    }

    /// Create a new, empty temp file next to `target`. `create_new` means
    /// O_CREAT|O_EXCL, which never follows or reuses an existing entry.
    fn create_temp(target: &Path) -> io::Result<(PathBuf, File)> {
        let dir = target.parent().ok_or_else(|| {
            io::Error::new(io::ErrorKind::InvalidInput, "target has no parent directory")
        })?;
        for _ in 0..8 {
            let tmp = temp_path_in(dir);
            match OpenOptions::new().write(true).create_new(true).open(&tmp) {
                Ok(f) => return Ok((tmp, f)),
                Err(e) if e.kind() == io::ErrorKind::AlreadyExists => continue,
                Err(e) => return Err(e),
            }
        }
        Err(io::Error::new(io::ErrorKind::AlreadyExists, "could not allocate a temp file"))
    }

    /// Rename `tmp` over `target`, removing `tmp` on failure. A symlink at
    /// `target` is replaced, not followed; a directory is refused.
    fn commit(tmp: &Path, target: &Path) -> io::Result<()> {
        let result = match fs::symlink_metadata(target) {
            Ok(md) if md.is_dir() => Err(io::Error::new(
                io::ErrorKind::InvalidInput,
                "target is a directory",
            )),
            Ok(md) => {
                // Keep the plain rwx bits of a regular file being replaced (an
                // executable script stays executable); never setuid/setgid.
                #[cfg(unix)]
                {
                    use std::os::unix::fs::PermissionsExt;
                    if md.is_file() {
                        let mode = md.permissions().mode() & 0o777;
                        let _ = fs::set_permissions(tmp, fs::Permissions::from_mode(mode));
                    }
                }
                #[cfg(not(unix))]
                let _ = md;
                fs::rename(tmp, target)
            }
            Err(_) => fs::rename(tmp, target),
        };
        if result.is_err() {
            let _ = fs::remove_file(tmp);
        }
        result
    }

    /// Write `target` by filling a fresh temp file and renaming it into place.
    pub fn write_atomic<T>(
        target: &Path,
        fill: impl FnOnce(&mut File) -> io::Result<T>,
    ) -> io::Result<T> {
        if fs::symlink_metadata(target).map(|m| m.is_dir()).unwrap_or(false) {
            return Err(io::Error::new(io::ErrorKind::InvalidInput, "target is a directory"));
        }
        let (tmp, mut file) = create_temp(target)?;
        let value = match fill(&mut file).and_then(|v| file.flush().map(|_| v)) {
            Ok(v) => v,
            Err(e) => {
                drop(file);
                let _ = fs::remove_file(&tmp);
                return Err(e);
            }
        };
        drop(file);
        commit(&tmp, target)?;
        Ok(value)
    }

    /// Copy `src` to `dst` via temp + rename. `src` must be daemon-controlled
    /// (it is opened normally); `dst` may sit in a tenant directory.
    pub fn copy_atomic(src: &Path, dst: &Path) -> io::Result<u64> {
        let mut input = File::open(src)?;
        write_atomic(dst, |out| io::copy(&mut input, out))
    }

    /// Async counterpart of the temp-file half of [`write_atomic`], for
    /// streaming writers. Finish with [`commit_async`].
    pub async fn create_temp_async(target: &Path) -> io::Result<(PathBuf, tokio::fs::File)> {
        let target = target.to_path_buf();
        let (tmp, file) = tokio::task::spawn_blocking(move || create_temp(&target))
            .await
            .map_err(io::Error::other)??;
        Ok((tmp, tokio::fs::File::from_std(file)))
    }

    /// Rename a temp file from [`create_temp_async`] over `target`.
    pub async fn commit_async(tmp: PathBuf, target: PathBuf) -> io::Result<()> {
        tokio::task::spawn_blocking(move || commit(&tmp, &target))
            .await
            .map_err(io::Error::other)?
    }

    /// Walk `rel` below `base` one component at a time, creating missing
    /// directories (recorded in `created`). Returns `Ok(None)` if a component
    /// is a symlink or not a directory: the chain is never followed through a
    /// link, so a planted symlink cannot redirect a write outside `base`.
    pub fn ensure_dir_chain(
        base: &Path,
        rel: &Path,
        created: &mut Vec<PathBuf>,
    ) -> io::Result<Option<PathBuf>> {
        let mut cur = base.to_path_buf();
        for comp in rel.components() {
            match comp {
                Component::Normal(name) => cur.push(name),
                Component::CurDir => continue,
                _ => return Ok(None),
            }
            match fs::symlink_metadata(&cur) {
                Ok(md) if md.is_dir() => {}
                Ok(_) => return Ok(None),
                Err(e) if e.kind() == io::ErrorKind::NotFound => match fs::create_dir(&cur) {
                    Ok(()) => created.push(cur.clone()),
                    // Lost a race with a concurrent creator: accept only a real directory.
                    Err(e) if e.kind() == io::ErrorKind::AlreadyExists => {
                        if !fs::symlink_metadata(&cur).map(|m| m.is_dir()).unwrap_or(false) {
                            return Ok(None);
                        }
                    }
                    Err(e) => return Err(e),
                },
                Err(e) => return Err(e),
            }
        }
        Ok(Some(cur))
    }

    /// True if a zip entry's unix mode marks it as a symlink (S_IFLNK).
    fn is_link_mode(mode: Option<u32>) -> bool {
        mode.is_some_and(|m| (m & 0o170000) == 0o120000)
    }

    /// Extract `archive` below `base` without ever following a symlink.
    ///
    /// Unsafe names (absolute, `..`), symlink entries and entries whose path
    /// crosses an existing symlink are skipped. Files are written via temp +
    /// rename. The entry count and the total bytes written (counted on the
    /// actual decompressed stream, not just the declared sizes) are capped by
    /// `limits`; exceeding a cap aborts and removes what this call created.
    pub fn extract_zip<R: Read + Seek>(
        archive: &mut zip::ZipArchive<R>,
        base: &Path,
        limits: ExtractLimits,
    ) -> Result<ExtractStats, String> {
        if archive.len() > limits.max_entries {
            return Err(format!(
                "archive has {} entries; the limit is {}",
                archive.len(),
                limits.max_entries
            ));
        }
        let base = base.canonicalize().map_err(|e| format!("canonicalize base: {e}"))?;
        let mut created_files = Vec::new();
        let mut created_dirs = Vec::new();
        let result = extract_entries(archive, &base, limits, &mut created_files, &mut created_dirs);
        if result.is_err() {
            for f in created_files.iter().rev() {
                let _ = fs::remove_file(f);
            }
            for d in created_dirs.iter().rev() {
                let _ = fs::remove_dir(d);
            }
        }
        result
    }

    fn extract_entries<R: Read + Seek>(
        archive: &mut zip::ZipArchive<R>,
        base: &Path,
        limits: ExtractLimits,
        created_files: &mut Vec<PathBuf>,
        created_dirs: &mut Vec<PathBuf>,
    ) -> Result<ExtractStats, String> {
        let mut stats = ExtractStats::default();
        let mut remaining = limits.max_total_bytes;
        let too_big = || "archive exceeds the extraction size limit".to_string();

        for i in 0..archive.len() {
            let mut entry = archive.by_index(i).map_err(|e| format!("read entry: {e}"))?;

            // enclosed_name drops absolute prefixes and refuses `..` traversal.
            let rel = match entry.enclosed_name() {
                Some(p) => p,
                None => {
                    stats.skipped += 1;
                    continue;
                }
            };
            if is_link_mode(entry.unix_mode()) {
                stats.skipped += 1;
                continue;
            }

            if entry.is_dir() {
                match ensure_dir_chain(base, &rel, created_dirs).map_err(|e| format!("mkdir: {e}"))? {
                    Some(_) => {}
                    None => stats.skipped += 1,
                }
                continue;
            }

            let name = match rel.file_name() {
                Some(n) => n.to_os_string(),
                None => {
                    stats.skipped += 1;
                    continue;
                }
            };
            if entry.size() > remaining {
                return Err(too_big());
            }

            // Containment is decided before anything is created: the parent
            // chain is built component by component and refuses symlinks.
            let parent_rel = rel.parent().unwrap_or(Path::new(""));
            let parent = match ensure_dir_chain(base, parent_rel, created_dirs)
                .map_err(|e| format!("mkdir: {e}"))?
            {
                Some(p) => p,
                None => {
                    stats.skipped += 1;
                    continue;
                }
            };
            let out_path = parent.join(name);
            let existed = match fs::symlink_metadata(&out_path) {
                Ok(md) if md.is_dir() => {
                    stats.skipped += 1;
                    continue;
                }
                Ok(_) => true,
                Err(_) => false,
            };

            let budget = remaining;
            let written = write_atomic(&out_path, |out| {
                let n = io::copy(&mut (&mut entry).take(budget + 1), out)?;
                if n > budget {
                    return Err(io::Error::other("size limit"));
                }
                Ok(n)
            });
            let written = match written {
                Ok(n) => n,
                Err(e) if e.to_string() == "size limit" => return Err(too_big()),
                Err(e) => return Err(format!("write {}: {e}", out_path.display())),
            };
            remaining -= written;
            if !existed {
                created_files.push(out_path);
            }
            stats.written += 1;
        }
        Ok(stats)
    }

    /// Add the contents of `current` (below `base`) to `writer`.
    ///
    /// Symlinks are skipped, never followed, as are non-regular files (a FIFO
    /// would block the read forever), the daemon's own temp files and any path
    /// in `exclude`. Files are streamed and opened with O_NOFOLLOW.
    pub fn zip_tree<W: Write + Seek>(
        writer: &mut zip::ZipWriter<W>,
        base: &Path,
        current: &Path,
        options: zip::write::SimpleFileOptions,
        exclude: &[PathBuf],
    ) -> Result<(), String> {
        for entry in fs::read_dir(current).map_err(|e| format!("read dir: {e}"))? {
            let entry = entry.map_err(|e| format!("dir entry: {e}"))?;
            // DirEntry::file_type does not follow symlinks.
            let ft = entry.file_type().map_err(|e| format!("file type: {e}"))?;
            let path = entry.path();
            if ft.is_symlink() || exclude.contains(&path) {
                continue;
            }
            if entry.file_name().to_str().is_some_and(is_temp_name) {
                continue;
            }
            let rel = path
                .strip_prefix(base)
                .map_err(|e| format!("strip prefix: {e}"))?
                .to_string_lossy()
                .replace('\\', "/");

            if ft.is_dir() {
                writer
                    .add_directory(format!("{rel}/"), options)
                    .map_err(|e| format!("add dir: {e}"))?;
                zip_tree(writer, base, &path, options, exclude)?;
            } else if ft.is_file() {
                let mut file = match open_nofollow(&path) {
                    Ok(f) => f,
                    // Swapped for a symlink since the listing: skip it.
                    Err(_) if is_symlink(&path) => continue,
                    Err(e) => return Err(format!("open {rel}: {e}")),
                };
                let len = file.metadata().map(|m| m.len()).unwrap_or(0);
                let opts = options.large_file(len >= u32::MAX as u64);
                writer.start_file(&rel, opts).map_err(|e| format!("start file: {e}"))?;
                io::copy(&mut file, writer).map_err(|e| format!("write {rel}: {e}"))?;
            }
        }
        Ok(())
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// File browser path security
// ─────────────────────────────────────────────────────────────────────────────

type PathError = (StatusCode, Json<serde_json::Value>);

fn path_error(status: StatusCode, msg: &str) -> PathError {
    (status, Json(serde_json::json!({"error": msg})))
}

/// Server ids are panel UUIDs. Requiring the canonical hyphenated form keeps
/// them usable as a single, inert path component.
pub(crate) fn valid_server_id(id: &str) -> bool {
    id.len() == 36 && uuid::Uuid::try_parse(id).is_ok()
}

/// Resolve a relative `user_path` within the server's base directory, ensuring
/// the result does not escape the base via `..` or symlinks.
///
/// Returns the canonicalized absolute path on success, or an error response.
async fn resolve_safe_path(
    servers_base_dir: &str,
    server_id: &str,
    user_path: &str,
) -> Result<PathBuf, PathError> {
    resolve_in_server(servers_base_dir, server_id, user_path)
        .await
        .map(|(_, p)| p)
}

/// Like [`resolve_safe_path`], but rejects the server root itself, for
/// operations that need a child path (write, delete, rename, mkdir).
async fn resolve_safe_child(
    servers_base_dir: &str,
    server_id: &str,
    user_path: &str,
) -> Result<PathBuf, PathError> {
    let (root, p) = resolve_in_server(servers_base_dir, server_id, user_path).await?;
    if p == root {
        return Err(path_error(
            StatusCode::BAD_REQUEST,
            "this operation needs a path below the server root",
        ));
    }
    Ok(p)
}

/// Returns `(canonical server root, resolved path)`.
///
/// The final component is never a symlink: an existing link is refused (even
/// one pointing inside the root), and for a new path the parent is
/// canonicalized and must lie within the root.
async fn resolve_in_server(
    servers_base_dir: &str,
    server_id: &str,
    user_path: &str,
) -> Result<(PathBuf, PathBuf), PathError> {
    if !valid_server_id(server_id) {
        return Err(path_error(StatusCode::BAD_REQUEST, "invalid server id"));
    }
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
    if target == canonical_base {
        return Ok((canonical_base.clone(), canonical_base));
    }

    let symlink_refused = || path_error(StatusCode::FORBIDDEN, "symbolic links are not allowed");

    // symlink_metadata, not exists(): exists() follows links, which would let
    // a dangling link stand in for a "new" path and redirect the write.
    let resolved = match std::fs::symlink_metadata(&target) {
        Ok(md) if md.file_type().is_symlink() => return Err(symlink_refused()),
        // Exists: canonicalize for a definitive check (resolves any symlinked
        // directory in the middle of the path).
        Ok(_) => target.canonicalize().map_err(|e| {
            warn!(%e, "failed to canonicalize target path");
            path_error(StatusCode::BAD_REQUEST, "invalid path")
        })?,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => {
            // The target doesn't exist yet; canonicalize its parent directory.
            let parent = target
                .parent()
                .ok_or_else(|| path_error(StatusCode::BAD_REQUEST, "invalid path"))?;
            let canonical_parent = match parent.canonicalize() {
                Ok(p) => p,
                Err(e) if e.kind() == std::io::ErrorKind::NotFound => {
                    return Err(path_error(StatusCode::NOT_FOUND, "parent directory does not exist"));
                }
                Err(e) => {
                    warn!(%e, "failed to canonicalize parent path");
                    return Err(path_error(StatusCode::BAD_REQUEST, "invalid path"));
                }
            };
            if !canonical_parent.starts_with(&canonical_base) {
                return Err(path_error(StatusCode::FORBIDDEN, "path traversal is not allowed"));
            }
            // Reconstruct the full path using the canonical parent + file name.
            let file_name = target
                .file_name()
                .ok_or_else(|| path_error(StatusCode::BAD_REQUEST, "invalid path"))?;
            let joined = canonical_parent.join(file_name);
            // Under the canonical parent the final component must still be
            // absent or a non-link (a dangling symlink is refused here).
            if safe_fs::is_symlink(&joined) {
                return Err(symlink_refused());
            }
            joined
        }
        Err(e) => {
            warn!(%e, "failed to stat target path");
            return Err(path_error(StatusCode::BAD_REQUEST, "invalid path"));
        }
    };

    if !resolved.starts_with(&canonical_base) {
        return Err(path_error(StatusCode::FORBIDDEN, "path traversal is not allowed"));
    }

    Ok((canonical_base, resolved))
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
            .map(|t| {
                let dt: chrono::DateTime<chrono::Utc> = t.into();
                dt.to_rfc3339()
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

    let target = match resolve_safe_child(&state.servers_base_dir, &server_id, &req.path).await {
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

    // Temp + rename, never a truncating write: the target may be a hardlink
    // to a file outside this server's directory.
    let content = req.content;
    let result = tokio::task::spawn_blocking(move || {
        safe_fs::write_atomic(&target, |f| std::io::Write::write_all(f, content.as_bytes()))
    })
    .await;

    match result {
        Ok(Ok(())) => ActionResponse::ok("file written successfully").into_response(),
        Ok(Err(e)) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(serde_json::json!({"error": format!("failed to write file: {e}")})),
        )
            .into_response(),
        Err(e) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(serde_json::json!({"error": format!("write task failed: {e}")})),
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

    let target = match resolve_safe_child(&state.servers_base_dir, &server_id, rel_path).await {
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
    let target = match resolve_safe_child(&state.servers_base_dir, &server_id, &req.path).await {
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

    let source = match resolve_safe_child(&state.servers_base_dir, &server_id, &req.path).await {
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

    // symlink_metadata so a dangling symlink also counts as "exists".
    if std::fs::symlink_metadata(&dest).is_ok() {
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

    let mut saved = 0u32;

    while let Ok(Some(mut field)) = multipart.next_field().await {
        // The multipart filename may carry a relative sub-path (e.g.
        // `mods/foo.jar`) when a whole folder is uploaded via the browser's
        // `webkitdirectory` input. Sanitize each component and rebuild a safe
        // destination underneath the (already validated) target directory.
        let raw_name = match field.file_name() {
            Some(name) => name.to_string(),
            None => continue,
        };

        let dest = match safe_join(&target_dir, &raw_name) {
            Ok(p) => p,
            Err(msg) => {
                return (
                    StatusCode::BAD_REQUEST,
                    Json(serde_json::json!({"error": msg})),
                )
                    .into_response();
            }
        };

        // Create parent directories for nested uploads one component at a
        // time, refusing symlinks: create_dir_all would follow a planted link
        // (e.g. `mods -> /etc`) and the file would land outside the server.
        let rel = dest.strip_prefix(&target_dir).unwrap_or(&dest).to_path_buf();
        let file_name = match rel.file_name() {
            Some(n) => n.to_os_string(),
            None => {
                return (
                    StatusCode::BAD_REQUEST,
                    Json(serde_json::json!({"error": format!("invalid filename: {raw_name}")})),
                )
                    .into_response();
            }
        };
        let parent_rel = rel.parent().map(|p| p.to_path_buf()).unwrap_or_default();
        let chain_base = target_dir.clone();
        let parent = match tokio::task::spawn_blocking(move || {
            safe_fs::ensure_dir_chain(&chain_base, &parent_rel, &mut Vec::new())
        })
        .await
        {
            Ok(Ok(Some(p))) => p,
            Ok(Ok(None)) => {
                return (
                    StatusCode::FORBIDDEN,
                    Json(serde_json::json!({"error": "upload path crosses a symbolic link or a file"})),
                )
                    .into_response();
            }
            Ok(Err(e)) => {
                return (
                    StatusCode::INTERNAL_SERVER_ERROR,
                    Json(serde_json::json!({"error": format!("failed to create directory: {e}")})),
                )
                    .into_response();
            }
            Err(e) => {
                return (
                    StatusCode::INTERNAL_SERVER_ERROR,
                    Json(serde_json::json!({"error": format!("directory task failed: {e}")})),
                )
                    .into_response();
            }
        };
        let dest = parent.join(file_name);
        match std::fs::symlink_metadata(&dest) {
            Ok(md) if md.file_type().is_symlink() => {
                return (
                    StatusCode::FORBIDDEN,
                    Json(serde_json::json!({"error": "symbolic links are not allowed"})),
                )
                    .into_response();
            }
            Ok(md) if md.is_dir() => {
                return (
                    StatusCode::BAD_REQUEST,
                    Json(serde_json::json!({"error": "upload target is a directory"})),
                )
                    .into_response();
            }
            _ => {}
        }

        // Stream the field's chunks straight to disk so uploads are bounded by
        // free disk space, not memory. No in-memory size cap. The data goes to
        // a fresh temp file that is renamed over the target at the end, so an
        // existing file (possibly a hardlink) is replaced, never written through.
        let (tmp, mut file) = match safe_fs::create_temp_async(&dest).await {
            Ok(v) => v,
            Err(e) => {
                return (
                    StatusCode::INTERNAL_SERVER_ERROR,
                    Json(serde_json::json!({"error": format!("failed to create file: {e}")})),
                )
                    .into_response();
            }
        };

        let streamed: Result<(), (StatusCode, String)> = async {
            loop {
                match field.chunk().await {
                    Ok(Some(chunk)) => file.write_all(&chunk).await.map_err(|e| {
                        (StatusCode::INTERNAL_SERVER_ERROR, format!("failed to write file: {e}"))
                    })?,
                    Ok(None) => break,
                    Err(e) => {
                        return Err((StatusCode::BAD_REQUEST, format!("failed to read upload: {e}")));
                    }
                }
            }
            file.flush().await.map_err(|e| {
                (StatusCode::INTERNAL_SERVER_ERROR, format!("failed to flush file: {e}"))
            })
        }
        .await;
        drop(file);

        if let Err((status, msg)) = streamed {
            let _ = tokio::fs::remove_file(&tmp).await;
            return (status, Json(serde_json::json!({"error": msg}))).into_response();
        }
        if let Err(e) = safe_fs::commit_async(tmp, dest).await {
            return (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({"error": format!("failed to store file: {e}")})),
            )
                .into_response();
        }

        saved += 1;
    }

    ActionResponse::ok(format!("{saved} file(s) uploaded")).into_response()
}

/// Join a user-supplied (possibly nested) relative file name onto an already
/// resolved, canonical base directory. Every path segment must be a plain
/// `Normal` component — this rejects `..`, `.`, absolute paths and drive
/// prefixes, so the result can never escape `base`.
fn safe_join(base: &std::path::Path, raw_name: &str) -> Result<PathBuf, String> {
    // Browsers send forward slashes in webkitRelativePath; normalize backslashes.
    let normalized = raw_name.replace('\\', "/");
    let mut dest = base.to_path_buf();
    let mut had_component = false;

    for segment in normalized.split('/') {
        if segment.is_empty() || segment == "." {
            continue;
        }
        let seg_path = std::path::Path::new(segment);
        let mut comps = seg_path.components();
        match (comps.next(), comps.next()) {
            (Some(std::path::Component::Normal(_)), None) => {
                dest.push(segment);
                had_component = true;
            }
            _ => return Err(format!("invalid filename: {raw_name}")),
        }
    }

    if !had_component {
        return Err(format!("invalid filename: {raw_name}"));
    }

    Ok(dest)
}

/// Build an RFC 6266 `attachment` header value. The quoted `filename` is an
/// ASCII fallback with quotes, backslashes and control characters replaced by
/// `_` (a tenant-chosen name cannot break out of the quotes or inject header
/// syntax); `filename*` carries the exact name percent-encoded as UTF-8.
fn content_disposition_attachment(name: &str) -> String {
    let fallback: String = name
        .chars()
        .map(|c| match c {
            ' '..='~' if c != '"' && c != '\\' => c,
            _ => '_',
        })
        .collect();
    let mut encoded = String::with_capacity(name.len());
    for b in name.bytes() {
        // RFC 5987 attr-char.
        if b.is_ascii_alphanumeric() || b"!#$&+-.^_`|~".contains(&b) {
            encoded.push(b as char);
        } else {
            encoded.push_str(&format!("%{b:02X}"));
        }
    }
    format!("attachment; filename=\"{fallback}\"; filename*=UTF-8''{encoded}")
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
                    content_disposition_attachment(&file_name),
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

    // Run extraction in a blocking task. safe_fs::extract_zip guards against
    // zip-slip, symlink entries, planted symlinks and decompression bombs.
    let result = tokio::task::spawn_blocking(move || -> Result<usize, String> {
        let file = safe_fs::open_nofollow(&archive_path).map_err(|e| format!("open archive: {e}"))?;
        let mut archive = zip::ZipArchive::new(file).map_err(|e| format!("read archive: {e}"))?;
        let stats = safe_fs::extract_zip(&mut archive, &extract_dir, safe_fs::ExtractLimits::default())?;
        Ok(stats.written)
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
    let (root, target) = match resolve_in_server(&state.servers_base_dir, &server_id, &req.path).await {
        Ok(p) => p,
        Err(e) => return e.into_response(),
    };

    if std::fs::symlink_metadata(&target).is_err() {
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
    // Next to the source, except for the server root itself: its parent is
    // outside the sandbox, so that archive goes inside the root.
    let archive_dir = if target == root {
        root.clone()
    } else {
        match target.parent() {
            Some(p) => p.to_path_buf(),
            None => return ActionResponse::err("invalid path").into_response(),
        }
    };
    let archive_path = archive_dir.join(&archive_name);
    let source = target.clone();

    let result = tokio::task::spawn_blocking(move || -> Result<String, String> {
        if safe_fs::is_symlink(&archive_path) {
            return Err("archive target is a symbolic link".to_string());
        }
        let options = zip::write::SimpleFileOptions::default()
            .compression_method(zip::CompressionMethod::Deflated);
        // Never archive the archive being written.
        let exclude = vec![archive_path.clone()];

        let written = safe_fs::write_atomic(&archive_path, |file| {
            let mut zip_writer = zip::ZipWriter::new(&mut *file);
            let zipped = if source.is_file() {
                let name = source.file_name().unwrap_or_default().to_string_lossy().to_string();
                safe_fs::open_nofollow(&source)
                    .map_err(|e| format!("read file: {e}"))
                    .and_then(|mut f| {
                        zip_writer
                            .start_file(&name, options.large_file(true))
                            .map_err(|e| format!("start file: {e}"))?;
                        std::io::copy(&mut f, &mut zip_writer).map_err(|e| format!("write: {e}"))?;
                        Ok(())
                    })
            } else {
                safe_fs::zip_tree(&mut zip_writer, &source, &source, options, &exclude)
            };
            zipped
                .and_then(|_| zip_writer.finish().map(|_| ()).map_err(|e| format!("finish archive: {e}")))
                .map_err(std::io::Error::other)
        });
        written.map_err(|e| format!("create archive: {e}"))?;
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
// Backup handlers — full-server archive to a dedicated backups directory
// ─────────────────────────────────────────────────────────────────────────────

#[derive(Deserialize)]
struct BackupRequest {
    backup_id: String,
}

#[derive(Deserialize)]
struct BackupQuery {
    backup_id: Option<String>,
}

#[derive(Serialize)]
struct BackupCreatedResponse {
    success: bool,
    size_bytes: u64,
    storage_path: String,
}

/// Validate a backup id (a UUID from the panel) so it cannot be used to escape
/// the backups directory. Only hex digits and dashes are allowed.
fn valid_backup_id(id: &str) -> bool {
    !id.is_empty()
        && id.len() <= 64
        && id.chars().all(|c| c.is_ascii_hexdigit() || c == '-')
}

/// Absolute path of a backup archive: `{backups_base_dir}/{server_id}/{backup_id}.zip`.
fn backup_archive_path(backups_base_dir: &str, server_id: &str, backup_id: &str) -> PathBuf {
    PathBuf::from(backups_base_dir)
        .join(server_id)
        .join(format!("{backup_id}.zip"))
}

/// Recursively zip a directory into `archive_path`. Blocking; call under spawn_blocking.
/// Symlinks inside `source` are skipped, never followed: a tenant link to
/// `/etc` or another server's directory must not end up in the backup.
fn zip_dir_to(source: &std::path::Path, archive_path: &std::path::Path) -> Result<u64, String> {
    if let Some(parent) = archive_path.parent() {
        std::fs::create_dir_all(parent).map_err(|e| format!("create backup dir: {e}"))?;
    }
    let options = zip::write::SimpleFileOptions::default()
        .compression_method(zip::CompressionMethod::Deflated);

    safe_fs::write_atomic(archive_path, |file| {
        let mut zip_writer = zip::ZipWriter::new(&mut *file);
        safe_fs::zip_tree(&mut zip_writer, source, source, options, &[])
            .and_then(|_| zip_writer.finish().map(|_| ()).map_err(|e| format!("finish archive: {e}")))
            .map_err(std::io::Error::other)
    })
    .map_err(|e| format!("create archive: {e}"))?;

    let size = std::fs::metadata(archive_path).map(|m| m.len()).unwrap_or(0);
    Ok(size)
}

/// `POST /servers/:id/backup` — archive the whole server data dir into the
/// backups directory. Body: `{"backup_id": "<uuid>"}`.
async fn create_backup(
    State(state): State<AppState>,
    Path(server_id): Path<String>,
    Json(req): Json<BackupRequest>,
) -> impl IntoResponse {
    if !valid_backup_id(&req.backup_id) {
        return (StatusCode::BAD_REQUEST, Json(serde_json::json!({"error": "invalid backup_id"}))).into_response();
    }

    // The server data dir is validated the same way as every other file op.
    let source = match resolve_safe_path(&state.servers_base_dir, &server_id, "/").await {
        Ok(p) => p,
        Err(e) => return e.into_response(),
    };
    if !source.is_dir() {
        return (StatusCode::NOT_FOUND, Json(serde_json::json!({"error": "server data directory not found"}))).into_response();
    }

    let archive_path = backup_archive_path(&state.backups_base_dir, &server_id, &req.backup_id);
    let storage_path = archive_path.to_string_lossy().to_string();

    let result = tokio::task::spawn_blocking(move || zip_dir_to(&source, &archive_path)).await;

    match result {
        Ok(Ok(size_bytes)) => Json(BackupCreatedResponse { success: true, size_bytes, storage_path }).into_response(),
        Ok(Err(e)) => (StatusCode::INTERNAL_SERVER_ERROR, Json(serde_json::json!({"error": e}))).into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, Json(serde_json::json!({"error": format!("backup task failed: {e}")}))).into_response(),
    }
}

/// `POST /servers/:id/backup/restore` — extract a backup archive back into the
/// server data dir. Body: `{"backup_id": "<uuid>"}`.
async fn restore_backup(
    State(state): State<AppState>,
    Path(server_id): Path<String>,
    Json(req): Json<BackupRequest>,
) -> impl IntoResponse {
    if !valid_backup_id(&req.backup_id) {
        return (StatusCode::BAD_REQUEST, Json(serde_json::json!({"error": "invalid backup_id"}))).into_response();
    }

    let dest = match resolve_safe_path(&state.servers_base_dir, &server_id, "/").await {
        Ok(p) => p,
        Err(e) => return e.into_response(),
    };
    if !dest.is_dir() {
        return (StatusCode::NOT_FOUND, Json(serde_json::json!({"error": "server data directory not found"}))).into_response();
    }

    let archive_path = backup_archive_path(&state.backups_base_dir, &server_id, &req.backup_id);
    if !archive_path.is_file() {
        return (StatusCode::NOT_FOUND, Json(serde_json::json!({"error": "backup archive not found"}))).into_response();
    }

    let result = tokio::task::spawn_blocking(move || -> Result<usize, String> {
        let file = std::fs::File::open(&archive_path).map_err(|e| format!("open archive: {e}"))?;
        let mut archive = zip::ZipArchive::new(file).map_err(|e| format!("read archive: {e}"))?;
        let stats = safe_fs::extract_zip(&mut archive, &dest, safe_fs::ExtractLimits::default())?;
        Ok(stats.written)
    })
    .await;

    match result {
        Ok(Ok(count)) => ActionResponse::ok(format!("restored {count} entries")).into_response(),
        Ok(Err(e)) => (StatusCode::INTERNAL_SERVER_ERROR, Json(serde_json::json!({"error": e}))).into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, Json(serde_json::json!({"error": format!("restore task failed: {e}")}))).into_response(),
    }
}

/// `GET /servers/:id/backup/download?backup_id=<uuid>` — stream a backup archive.
async fn download_backup(
    State(state): State<AppState>,
    Path(server_id): Path<String>,
    Query(query): Query<BackupQuery>,
) -> impl IntoResponse {
    let backup_id = match query.backup_id {
        Some(id) if valid_backup_id(&id) && valid_server_id(&server_id) => id,
        _ => return (StatusCode::BAD_REQUEST, Json(serde_json::json!({"error": "invalid server id or invalid or missing backup_id"}))).into_response(),
    };

    let archive_path = backup_archive_path(&state.backups_base_dir, &server_id, &backup_id);
    if !archive_path.is_file() {
        return (StatusCode::NOT_FOUND, Json(serde_json::json!({"error": "backup archive not found"}))).into_response();
    }

    match tokio::fs::read(&archive_path).await {
        Ok(data) => {
            let headers = [
                (axum::http::header::CONTENT_TYPE, "application/zip".to_string()),
                (axum::http::header::CONTENT_DISPOSITION, content_disposition_attachment(&format!("{backup_id}.zip"))),
            ];
            (headers, data).into_response()
        }
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, Json(serde_json::json!({"error": format!("failed to read archive: {e}")}))).into_response(),
    }
}

/// `DELETE /servers/:id/backup?backup_id=<uuid>` — remove a backup archive.
/// Missing files are treated as success (idempotent).
async fn delete_backup(
    State(state): State<AppState>,
    Path(server_id): Path<String>,
    Query(query): Query<BackupQuery>,
) -> impl IntoResponse {
    let backup_id = match query.backup_id {
        Some(id) if valid_backup_id(&id) && valid_server_id(&server_id) => id,
        _ => return (StatusCode::BAD_REQUEST, Json(serde_json::json!({"error": "invalid server id or invalid or missing backup_id"}))).into_response(),
    };

    let archive_path = backup_archive_path(&state.backups_base_dir, &server_id, &backup_id);
    match tokio::fs::remove_file(&archive_path).await {
        Ok(_) => ActionResponse::ok("backup deleted").into_response(),
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => ActionResponse::ok("backup already absent").into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, Json(serde_json::json!({"error": format!("failed to delete archive: {e}")}))).into_response(),
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Migration import — receive a server archive and reconstruct its data dir
// ─────────────────────────────────────────────────────────────────────────────

/// `POST /servers/:id/import` — accept a multipart "file" (a zip of a server's
/// data dir) and extract it into `{servers_base_dir}/{server_id}`, creating the
/// directory if needed. Used as the receiving half of a cross-node migration.
async fn import_server(
    State(state): State<AppState>,
    Path(server_id): Path<String>,
    mut multipart: Multipart,
) -> impl IntoResponse {
    // The server id comes from the panel; guard against traversal regardless.
    if !valid_server_id(&server_id) {
        return (StatusCode::BAD_REQUEST, Json(serde_json::json!({"error": "invalid server id"}))).into_response();
    }

    let dest = PathBuf::from(&state.servers_base_dir).join(&server_id);
    if let Err(e) = tokio::fs::create_dir_all(&dest).await {
        return (StatusCode::INTERNAL_SERVER_ERROR, Json(serde_json::json!({"error": format!("create server dir: {e}")}))).into_response();
    }

    // Read the uploaded archive into memory (bounded), then extract in a blocking task.
    const MAX_IMPORT: usize = 2 * 1024 * 1024 * 1024; // 2 GiB
    let mut archive_bytes: Option<Vec<u8>> = None;
    while let Ok(Some(field)) = multipart.next_field().await {
        if field.name() != Some("file") {
            continue;
        }
        match field.bytes().await {
            Ok(data) => {
                if data.len() > MAX_IMPORT {
                    return (StatusCode::BAD_REQUEST, Json(serde_json::json!({"error": "import archive exceeds size limit"}))).into_response();
                }
                archive_bytes = Some(data.to_vec());
            }
            Err(e) => return (StatusCode::BAD_REQUEST, Json(serde_json::json!({"error": format!("failed to read upload: {e}")}))).into_response(),
        }
    }

    let bytes = match archive_bytes {
        Some(b) => b,
        None => return (StatusCode::BAD_REQUEST, Json(serde_json::json!({"error": "missing file field"}))).into_response(),
    };

    let result = tokio::task::spawn_blocking(move || -> Result<usize, String> {
        let reader = std::io::Cursor::new(bytes);
        let mut archive = zip::ZipArchive::new(reader).map_err(|e| format!("read archive: {e}"))?;
        let stats = safe_fs::extract_zip(&mut archive, &dest, safe_fs::ExtractLimits::default())?;
        Ok(stats.written)
    })
    .await;

    match result {
        Ok(Ok(count)) => ActionResponse::ok(format!("imported {count} entries")).into_response(),
        Ok(Err(e)) => (StatusCode::INTERNAL_SERVER_ERROR, Json(serde_json::json!({"error": e}))).into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, Json(serde_json::json!({"error": format!("import task failed: {e}")}))).into_response(),
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

/// Constant-time token check. Both sides are hashed first so the comparison
/// runs over two fixed-length digests: neither the position of the first
/// mismatching byte nor the length of the expected token leaks via timing.
fn token_matches(presented: &str, expected: &str) -> bool {
    use sha2::{Digest, Sha256};
    if expected.is_empty() {
        return false;
    }
    let a = Sha256::digest(presented.as_bytes());
    let b = Sha256::digest(expected.as_bytes());
    a.iter().zip(b.iter()).fold(0u8, |acc, (x, y)| acc | (x ^ y)) == 0
}

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
        Some(t) if token_matches(t, &state.node_token) => next.run(req).await.into_response(),
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
        .route(
            "/servers/:id/files/upload",
            // Disable the 2 MB default body limit — uploads stream to disk and
            // may be multiple gigabytes (whole folders via webkitdirectory).
            post(upload_file).layer(axum::extract::DefaultBodyLimit::disable()),
        )
        .route("/servers/:id/files/download", get(download_file))
        .route("/servers/:id/files/extract", post(extract_archive))
        .route("/servers/:id/files/archive", post(create_archive))
        .route("/servers/:id/backup", post(create_backup).delete(delete_backup))
        .route("/servers/:id/backup/restore", post(restore_backup))
        .route("/servers/:id/backup/download", get(download_backup))
        .route("/servers/:id/import", post(import_server))
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

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Write;
    use std::sync::atomic::{AtomicU32, Ordering};

    static COUNTER: AtomicU32 = AtomicU32::new(0);

    const SID: &str = "123e4567-e89b-12d3-a456-426614174000";

    fn scratch_dir(tag: &str) -> PathBuf {
        let n = COUNTER.fetch_add(1, Ordering::SeqCst);
        let dir = std::env::temp_dir().join(format!("tp-http-test-{tag}-{}-{n}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).expect("create scratch dir");
        dir.canonicalize().unwrap()
    }

    fn zip_bytes(build: impl FnOnce(&mut zip::ZipWriter<std::io::Cursor<Vec<u8>>>)) -> Vec<u8> {
        let mut w = zip::ZipWriter::new(std::io::Cursor::new(Vec::new()));
        build(&mut w);
        w.finish().unwrap().into_inner()
    }

    fn opts() -> zip::write::SimpleFileOptions {
        zip::write::SimpleFileOptions::default().compression_method(zip::CompressionMethod::Deflated)
    }

    fn extract(
        bytes: Vec<u8>,
        base: &std::path::Path,
        limits: safe_fs::ExtractLimits,
    ) -> Result<safe_fs::ExtractStats, String> {
        let mut archive = zip::ZipArchive::new(std::io::Cursor::new(bytes)).unwrap();
        safe_fs::extract_zip(&mut archive, base, limits)
    }

    #[test]
    fn server_ids_must_be_canonical_uuids() {
        assert!(valid_server_id(SID));
        assert!(!valid_server_id(""));
        assert!(!valid_server_id(".."));
        assert!(!valid_server_id("../etc"));
        assert!(!valid_server_id("123e4567e89b12d3a456426614174000"));
        assert!(!valid_server_id("{123e4567-e89b-12d3-a456-426614174000}"));
    }

    #[test]
    fn token_compare_accepts_only_the_exact_token() {
        assert!(token_matches("secret-token", "secret-token"));
        assert!(!token_matches("secret-tokem", "secret-token"));
        assert!(!token_matches("secret", "secret-token"));
        assert!(!token_matches("", ""));
    }

    #[test]
    fn content_disposition_cannot_break_out_of_the_quotes() {
        let v = content_disposition_attachment("a\"b\\c\r\nX-Evil: 1;\u{fc}.txt");
        assert!(
            v.starts_with("attachment; filename=\"a_b_c__X-Evil: 1;_.txt\"; filename*=UTF-8''"),
            "{v}"
        );
        assert!(v.ends_with("a%22b%5Cc%0D%0AX-Evil%3A%201%3B%C3%BC.txt"), "{v}");
        assert!(axum::http::HeaderValue::from_str(&v).is_ok());
    }

    #[test]
    fn write_atomic_replaces_a_hardlink_instead_of_writing_through_it() {
        let dir = scratch_dir("hardlink");
        let shared = dir.join("shared.jar");
        std::fs::write(&shared, b"original").unwrap();
        std::fs::create_dir(dir.join("srv")).unwrap();
        let linked = dir.join("srv").join("HytaleServer.jar");
        std::fs::hard_link(&shared, &linked).unwrap();

        safe_fs::write_atomic(&linked, |f| f.write_all(b"tenant data")).unwrap();

        assert_eq!(std::fs::read(&shared).unwrap(), b"original");
        assert_eq!(std::fs::read(&linked).unwrap(), b"tenant data");
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[cfg(unix)]
    #[test]
    fn write_atomic_replaces_a_symlink_instead_of_following_it() {
        let dir = scratch_dir("symlink-write");
        let outside = dir.join("outside.txt");
        std::fs::write(&outside, b"keep").unwrap();
        let link = dir.join("link.txt");
        std::os::unix::fs::symlink(&outside, &link).unwrap();

        safe_fs::write_atomic(&link, |f| f.write_all(b"new")).unwrap();

        assert_eq!(std::fs::read(&outside).unwrap(), b"keep");
        assert!(!safe_fs::is_symlink(&link));
        assert_eq!(std::fs::read(&link).unwrap(), b"new");
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[cfg(unix)]
    #[tokio::test]
    async fn resolve_refuses_symlinks_and_dangling_links() {
        let base = scratch_dir("resolve");
        let root = base.join(SID);
        std::fs::create_dir_all(root.join("sub")).unwrap();
        let outside = scratch_dir("resolve-outside");
        std::os::unix::fs::symlink(&outside, root.join("escape")).unwrap();
        std::os::unix::fs::symlink(outside.join("nope"), root.join("dangling")).unwrap();
        std::os::unix::fs::symlink(root.join("sub"), root.join("inner")).unwrap();
        let b = base.to_str().unwrap();

        assert!(resolve_safe_path(b, SID, "/sub").await.is_ok());
        assert!(resolve_safe_path(b, SID, "/new.txt").await.is_ok());
        assert_eq!(resolve_safe_path(b, SID, "/escape").await.unwrap_err().0, StatusCode::FORBIDDEN);
        assert_eq!(resolve_safe_path(b, SID, "/escape/x").await.unwrap_err().0, StatusCode::FORBIDDEN);
        assert_eq!(resolve_safe_path(b, SID, "/dangling").await.unwrap_err().0, StatusCode::FORBIDDEN);
        // Even a link pointing inside the root is refused as a final component.
        assert_eq!(resolve_safe_path(b, SID, "/inner").await.unwrap_err().0, StatusCode::FORBIDDEN);
        assert_eq!(resolve_safe_path(b, "../x", "/").await.unwrap_err().0, StatusCode::BAD_REQUEST);
        assert!(resolve_safe_child(b, SID, "/").await.is_err());
        assert!(resolve_safe_child(b, SID, "/sub").await.is_ok());

        let _ = std::fs::remove_dir_all(&base);
        let _ = std::fs::remove_dir_all(&outside);
    }

    #[test]
    fn extract_writes_regular_entries_and_skips_traversal() {
        let base = scratch_dir("extract-ok");
        let bytes = zip_bytes(|w| {
            w.add_directory("dir/", opts()).unwrap();
            w.start_file("dir/a.txt", opts()).unwrap();
            w.write_all(b"hello").unwrap();
            w.start_file("../evil.txt", opts()).unwrap();
            w.write_all(b"x").unwrap();
        });
        let stats = extract(bytes, &base, safe_fs::ExtractLimits::default()).unwrap();
        assert_eq!(stats.written, 1);
        assert_eq!(std::fs::read(base.join("dir/a.txt")).unwrap(), b"hello");
        assert!(!base.parent().unwrap().join("evil.txt").exists());
        let _ = std::fs::remove_dir_all(&base);
    }

    #[cfg(unix)]
    #[test]
    fn extract_skips_symlink_entries_and_never_follows_planted_links() {
        let base = scratch_dir("extract-links");
        let outside = scratch_dir("extract-outside");
        std::fs::write(outside.join("victim.txt"), b"keep").unwrap();
        // Planted by the tenant before the extraction.
        std::os::unix::fs::symlink(&outside, base.join("mods")).unwrap();
        std::os::unix::fs::symlink(outside.join("victim.txt"), base.join("config.json")).unwrap();

        let bytes = zip_bytes(|w| {
            w.add_symlink("link", "/etc", opts()).unwrap();
            w.start_file("mods/victim.txt", opts()).unwrap();
            w.write_all(b"pwned").unwrap();
            w.start_file("config.json", opts()).unwrap();
            w.write_all(b"{}").unwrap();
        });
        let stats = extract(bytes, &base, safe_fs::ExtractLimits::default()).unwrap();

        assert_eq!(stats.written, 1, "only config.json is written");
        assert_eq!(stats.skipped, 2);
        assert!(std::fs::symlink_metadata(base.join("link")).is_err(), "symlink entry must not be created");
        assert_eq!(std::fs::read(outside.join("victim.txt")).unwrap(), b"keep");
        // The planted link was replaced by a regular file, not written through.
        assert!(!safe_fs::is_symlink(&base.join("config.json")));
        assert_eq!(std::fs::read(base.join("config.json")).unwrap(), b"{}");
        let _ = std::fs::remove_dir_all(&base);
        let _ = std::fs::remove_dir_all(&outside);
    }

    #[test]
    fn extract_aborts_on_size_limit_and_cleans_up() {
        let base = scratch_dir("extract-bomb");
        let bytes = zip_bytes(|w| {
            w.start_file("first/a.bin", opts()).unwrap();
            w.write_all(&[0u8; 600]).unwrap();
            w.start_file("second/b.bin", opts()).unwrap();
            w.write_all(&[0u8; 600]).unwrap();
        });
        let limits = safe_fs::ExtractLimits { max_total_bytes: 1000, max_entries: 10 };
        let err = extract(bytes, &base, limits).unwrap_err();
        assert!(err.contains("size limit"), "{err}");
        assert!(!base.join("first").exists(), "partial output must be removed");
        assert!(!base.join("second").exists());
        let _ = std::fs::remove_dir_all(&base);
    }

    #[test]
    fn extract_rejects_too_many_entries() {
        let base = scratch_dir("extract-count");
        let bytes = zip_bytes(|w| {
            for i in 0..5 {
                w.start_file(format!("f{i}"), opts()).unwrap();
            }
        });
        let limits = safe_fs::ExtractLimits { max_total_bytes: 1 << 20, max_entries: 4 };
        assert!(extract(bytes, &base, limits).unwrap_err().contains("entries"));
        let _ = std::fs::remove_dir_all(&base);
    }

    #[cfg(unix)]
    #[test]
    fn zip_tree_skips_symlinks() {
        let src = scratch_dir("zip-src");
        let outside = scratch_dir("zip-outside");
        std::fs::write(outside.join("secret"), b"s").unwrap();
        std::fs::write(src.join("real.txt"), b"r").unwrap();
        std::os::unix::fs::symlink(outside.join("secret"), src.join("leak")).unwrap();
        std::os::unix::fs::symlink(&outside, src.join("leakdir")).unwrap();

        let out_dir = scratch_dir("zip-out");
        let out = out_dir.join("b.zip");
        zip_dir_to(&src, &out).unwrap();

        let archive = zip::ZipArchive::new(std::fs::File::open(&out).unwrap()).unwrap();
        let names: Vec<&str> = archive.file_names().collect();
        assert_eq!(names, vec!["real.txt"]);
        let _ = std::fs::remove_dir_all(&src);
        let _ = std::fs::remove_dir_all(&outside);
        let _ = std::fs::remove_dir_all(&out_dir);
    }
}
