//! Off-site backup transport: runs `rclone` against a remote described by the
//! panel (OneDrive, Google Drive, Dropbox, S3, B2, SFTP, WebDAV).
//!
//! Secrets never appear on a command line (visible in `ps`): each operation
//! writes a throwaway rclone config file (0600, in a private 0700 directory)
//! and points rclone at it with `--config`.  rclone writes refreshed OAuth
//! tokens back into that file; they are read back afterwards and returned to
//! the panel as `config_updates` so it can persist them.
//!
//! rclone is always spawned directly with an argument array (no shell), with
//! `--` before positional arguments, under a timeout, and killed on drop.

use std::collections::BTreeMap;
use std::ffi::OsString;
use std::path::{Path, PathBuf};
use std::process::Stdio;
use std::time::Duration;

use serde::Deserialize;
use tokio::io::AsyncWriteExt;
use tokio::process::Command;

/// Name of the remote inside the generated config file.
pub const REMOTE_NAME: &str = "tp";

/// Backends the panel may configure.  Anything else is refused.
const ALLOWED_BACKENDS: &[&str] = &["onedrive", "drive", "dropbox", "s3", "b2", "sftp", "webdav"];

/// Option keys rclone may write back that the panel wants to persist.
const WRITE_BACK_KEYS: &[&str] = &["token", "drive_id", "drive_type"];

/// Upload / fetch of one archive.  Slightly below the API's 6h so the daemon
/// reports a clean timeout before the API gives up.
pub const TRANSFER_TIMEOUT: Duration = Duration::from_secs(5 * 3600 + 45 * 60);
/// Connection test (probe upload + delete) and OneDrive drive lookup.
pub const SHORT_TIMEOUT: Duration = Duration::from_secs(120);
/// Deleting one remote object.
pub const DELETE_TIMEOUT: Duration = Duration::from_secs(300);

const MAX_VALUE_LEN: usize = 16 * 1024;
const MAX_ERROR_LEN: usize = 2000;

/// Remote description sent by the panel (mirrors daemon.RemoteSpec in Go).
#[derive(Debug, Clone, Deserialize)]
pub struct RemoteSpec {
    pub backend: String,
    #[serde(default)]
    pub options: BTreeMap<String, String>,
    /// Option keys rclone stores obscured (passwords).
    #[serde(default)]
    pub obscure: Vec<String>,
    pub base_path: String,
}

pub type ConfigUpdates = BTreeMap<String, String>;

/// Path of the rclone binary (`TALEDAEMON_RCLONE_BIN` overrides, for tests).
fn rclone_bin() -> String {
    std::env::var("TALEDAEMON_RCLONE_BIN").unwrap_or_else(|_| "rclone".to_string())
}

// ─────────────────────────────────────────────────────────────────────────────
// Validation
// ─────────────────────────────────────────────────────────────────────────────

/// rclone option keys: lower-case identifiers.  Keys that make rclone run a
/// command or read a local file on this node are refused even if the panel
/// sends them (defence in depth — the panel has its own allowlist).
pub fn valid_option_key(key: &str) -> bool {
    let mut chars = key.chars();
    let first_ok = matches!(chars.next(), Some(c) if c.is_ascii_lowercase());
    first_ok
        && key.len() <= 64
        && key.chars().all(|c| c.is_ascii_lowercase() || c.is_ascii_digit() || c == '_')
        && key != "type"
        && key != "ssh"
        && !key.ends_with("_command")
        && !key.ends_with("_file")
        && !key.starts_with("config_")
}

/// Option values end up as `key = value` lines; a newline would let a value
/// inject further keys or sections.
pub fn valid_option_value(value: &str) -> bool {
    value.len() <= MAX_VALUE_LEN && !value.contains(['\r', '\n', '\0'])
}

/// The destination folder on the remote.  Mirrors the API's
/// ValidateRemoteBasePath: no `.`/`..`, no empty segments, no segment starting
/// with `-`, no control characters, an optional single leading `/`.
pub fn validate_base_path(p: &str) -> Result<(), String> {
    if p.is_empty() || p.len() > 512 {
        return Err("invalid base path length".into());
    }
    let rest = p.strip_prefix('/').unwrap_or(p);
    if rest.is_empty() {
        return Err("base path must name a folder".into());
    }
    for seg in rest.split('/') {
        if seg.is_empty() || seg == "." || seg == ".." {
            return Err("base path contains an empty, . or .. segment".into());
        }
        if seg.starts_with('-') {
            return Err("base path segments must not start with -".into());
        }
        if seg.trim() != seg {
            return Err("base path segments must not start or end with spaces".into());
        }
        if seg.chars().any(|c| c.is_control() || "\\:*?\"<>|".contains(c)) {
            return Err("base path contains a forbidden character".into());
        }
    }
    Ok(())
}

/// A single path component we generate (server id, backup file name).
pub fn valid_name_component(name: &str) -> bool {
    !name.is_empty()
        && name.len() <= 128
        && !name.starts_with('-')
        && !name.starts_with('.')
        && name.chars().all(|c| c.is_ascii_alphanumeric() || c == '-' || c == '_' || c == '.')
        && !name.contains("..")
}

pub fn validate_spec(spec: &RemoteSpec) -> Result<(), String> {
    if !ALLOWED_BACKENDS.contains(&spec.backend.as_str()) {
        return Err(format!("unsupported backend {:?}", spec.backend));
    }
    for (k, v) in &spec.options {
        if !valid_option_key(k) {
            return Err(format!("option {k:?} is not allowed"));
        }
        if !valid_option_value(v) {
            return Err(format!("option {k:?} has an invalid value"));
        }
    }
    for k in &spec.obscure {
        if !valid_option_key(k) {
            return Err(format!("option {k:?} is not allowed"));
        }
    }
    validate_base_path(&spec.base_path)
}

/// `<base>/<server_id>/<file_name>` — the object path inside the remote.
pub fn remote_object_path(base: &str, server_id: &str, file_name: &str) -> Result<String, String> {
    validate_base_path(base)?;
    if !valid_name_component(server_id) || !valid_name_component(file_name) {
        return Err("invalid server id or file name".into());
    }
    Ok(format!("{}/{}/{}", base.trim_end_matches('/'), server_id, file_name))
}

/// `tp:<path>` — the remote argument handed to rclone.
pub fn remote_arg(object_path: &str) -> String {
    format!("{REMOTE_NAME}:{object_path}")
}

// ─────────────────────────────────────────────────────────────────────────────
// Config file + argument building (pure, unit-tested)
// ─────────────────────────────────────────────────────────────────────────────

/// Render the rclone config file.  Values must already be validated.
pub fn render_config(backend: &str, options: &BTreeMap<String, String>) -> String {
    let mut out = format!("[{REMOTE_NAME}]\ntype = {backend}\n");
    for (k, v) in options {
        out.push_str(k);
        out.push_str(" = ");
        out.push_str(v);
        out.push('\n');
    }
    out
}

/// Read the `[tp]` section of an rclone config file.
pub fn parse_config_section(text: &str) -> BTreeMap<String, String> {
    let mut out = BTreeMap::new();
    let mut in_section = false;
    for line in text.lines() {
        let line = line.trim();
        if line.starts_with('[') && line.ends_with(']') {
            in_section = &line[1..line.len() - 1] == REMOTE_NAME;
            continue;
        }
        if !in_section || line.is_empty() || line.starts_with('#') || line.starts_with(';') {
            continue;
        }
        if let Some((k, v)) = line.split_once('=') {
            out.insert(k.trim().to_string(), v.trim().to_string());
        }
    }
    out
}

/// Keys among [`WRITE_BACK_KEYS`] whose value rclone changed.
pub fn diff_write_back(before: &BTreeMap<String, String>, after: &BTreeMap<String, String>) -> ConfigUpdates {
    WRITE_BACK_KEYS
        .iter()
        .filter_map(|k| {
            let new = after.get(*k)?;
            if new.is_empty() || before.get(*k) == Some(new) {
                None
            } else {
                Some((k.to_string(), new.clone()))
            }
        })
        .collect()
}

fn with_common(sub: &[&str], conf: &Path) -> Vec<OsString> {
    let mut args: Vec<OsString> = sub.iter().map(OsString::from).collect();
    args.push("--config".into());
    args.push(conf.as_os_str().to_owned());
    args
}

/// `rclone copyto --config C --retries 3 --low-level-retries 10 -- SRC DST`
pub fn copyto_args(conf: &Path, src: &OsString, dst: &OsString) -> Vec<OsString> {
    let mut args = with_common(&["copyto"], conf);
    for a in ["--retries", "3", "--low-level-retries", "10", "--stats", "0"] {
        args.push(a.into());
    }
    args.push("--".into());
    args.push(src.clone());
    args.push(dst.clone());
    args
}

/// `rclone deletefile --config C -- REMOTE`
pub fn deletefile_args(conf: &Path, remote: &str) -> Vec<OsString> {
    let mut args = with_common(&["deletefile"], conf);
    args.push("--".into());
    args.push(remote.into());
    args
}

/// A OneDrive drive id as returned by Microsoft Graph (personal ids are hex,
/// business ones look like `b!AbC-dE_f...`).
pub fn valid_drive_id(id: &str) -> bool {
    !id.is_empty()
        && id.len() <= 256
        && !id.starts_with('-')
        && id.chars().all(|c| c.is_ascii_alphanumeric() || "!-_.".contains(c))
}

/// Drive the OneDrive config state machine non-interactively (rclone
/// `config update --non-interactive`, see `rclone config create --help`):
/// keep the pasted token (`config_refresh_token=false`), pick "OneDrive
/// Personal or Business" (`config_type=onedrive`), and — once known — the
/// drive id.  Without a drive id rclone answers with the list of drives.
pub fn onedrive_resolve_args(conf: &Path, drive_id: Option<&str>) -> Vec<OsString> {
    let mut args = with_common(&["config", "update"], conf);
    args.push("--non-interactive".into());
    args.push("--".into());
    args.push(REMOTE_NAME.into());
    args.push("config_refresh_token=false".into());
    args.push("config_type=onedrive".into());
    if let Some(id) = drive_id {
        args.push(format!("config_driveid={id}").into());
        args.push("config_drive_ok=true".into());
    }
    args
}

#[derive(Debug, Deserialize)]
struct ConfigOutExample {
    #[serde(rename = "Value", default)]
    value: String,
    #[serde(rename = "Help", default)]
    help: String,
}

#[derive(Debug, Deserialize)]
struct ConfigOutOption {
    #[serde(rename = "Name", default)]
    name: String,
    #[serde(rename = "Examples", default)]
    examples: Option<Vec<ConfigOutExample>>,
}

#[derive(Debug, Deserialize)]
struct ConfigOut {
    #[serde(rename = "State", default)]
    state: String,
    #[serde(rename = "Option", default)]
    option: Option<ConfigOutOption>,
    #[serde(rename = "Error", default)]
    error: String,
}

/// Pick the drive from rclone's "Select drive you want to use" question.
/// The user's own OneDrive (personal or business) wins over SharePoint
/// libraries; otherwise the first drive.
pub fn pick_onedrive_drive(json: &str) -> Result<String, String> {
    let out: ConfigOut = serde_json::from_str(json.trim())
        .map_err(|e| format!("unexpected rclone config output: {e}"))?;
    if !out.error.is_empty() {
        return Err(out.error.trim().to_string());
    }
    let option = out
        .option
        .ok_or_else(|| format!("rclone did not ask for a drive (state {:?})", out.state))?;
    if option.name != "config_driveid" {
        return Err(format!("rclone asked an unexpected question ({})", option.name));
    }
    let examples = option.examples.unwrap_or_default();
    let chosen = examples
        .iter()
        .find(|e| e.help.ends_with("(personal)") || e.help.ends_with("(business)"))
        .or_else(|| examples.first())
        .ok_or_else(|| "no OneDrive drive found for this account".to_string())?;
    if !valid_drive_id(&chosen.value) {
        return Err("rclone returned an invalid drive id".into());
    }
    Ok(chosen.value.clone())
}

/// Condense rclone's stderr into the error text shown in the panel.
/// Drop rclone's `2026/10/23 23:20:31 ` log timestamp and the level tag.
fn strip_log_prefix(line: &str) -> &str {
    let line = line.trim();
    let b = line.as_bytes();
    let rest = if b.len() > 20 && b[4] == b'/' && b[7] == b'/' && b[10] == b' ' && b[13] == b':' && b[19] == b' ' {
        &line[20..]
    } else {
        line
    };
    for tag in ["NOTICE: ", "ERROR : ", "CRITICAL: ", "INFO  : "] {
        if let Some(r) = rest.strip_prefix(tag) {
            return r.trim();
        }
    }
    rest.trim()
}

/// rclone repeats the same error once per retry and ends with a "Failed to
/// <command>: <reason>" summary, which is what a user needs to see.
pub fn error_text(stderr: &str, fallback: &str) -> String {
    let lines: Vec<&str> = stderr.lines().map(strip_log_prefix).filter(|l| !l.is_empty()).collect();
    let summary: Vec<&str> = lines.iter().copied().filter(|l| l.contains("Failed to")).collect();
    let important: Vec<&str> = if !summary.is_empty() {
        summary
    } else {
        lines
            .iter()
            .copied()
            .filter(|l| l.contains("ERROR") || l.contains("CRITICAL") || l.contains("Failed") || l.contains("error"))
            .collect()
    };
    let source = if important.is_empty() { lines } else { important };
    let mut chosen: Vec<&str> = Vec::new();
    for l in source.iter().rev() {
        if !chosen.contains(l) {
            chosen.push(l);
        }
        if chosen.len() == 3 {
            break;
        }
    }
    chosen.reverse();
    let mut text = chosen.join("\n");
    if text.is_empty() {
        text = fallback.to_string();
    }
    if text.len() > MAX_ERROR_LEN {
        let mut cut = MAX_ERROR_LEN;
        while !text.is_char_boundary(cut) {
            cut -= 1;
        }
        text.truncate(cut);
        text.push_str("...");
    }
    text
}

// ─────────────────────────────────────────────────────────────────────────────
// Running rclone
// ─────────────────────────────────────────────────────────────────────────────

async fn run_rclone(args: Vec<OsString>, stdin: Option<&[u8]>, timeout: Duration) -> Result<String, String> {
    let mut cmd = Command::new(rclone_bin());
    cmd.args(&args)
        .stdin(if stdin.is_some() { Stdio::piped() } else { Stdio::null() })
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .kill_on_drop(true);
    // Ignore any RCLONE_* settings of the daemon's own environment: the
    // generated config file is the only source of truth.
    for (k, _) in std::env::vars_os() {
        if k.to_string_lossy().starts_with("RCLONE_") {
            cmd.env_remove(k);
        }
    }
    let mut child = cmd.spawn().map_err(|e| {
        if e.kind() == std::io::ErrorKind::NotFound {
            "rclone is not installed on this node (update the daemon image)".to_string()
        } else {
            format!("could not start rclone: {e}")
        }
    })?;
    if let Some(data) = stdin {
        if let Some(mut pipe) = child.stdin.take() {
            pipe.write_all(data).await.map_err(|e| format!("rclone stdin: {e}"))?;
            drop(pipe);
        }
    }
    let output = match tokio::time::timeout(timeout, child.wait_with_output()).await {
        Ok(Ok(o)) => o,
        Ok(Err(e)) => return Err(format!("rclone failed to run: {e}")),
        Err(_) => return Err(format!("rclone timed out after {} s", timeout.as_secs())),
    };
    let stdout = String::from_utf8_lossy(&output.stdout).to_string();
    if output.status.success() {
        Ok(stdout)
    } else {
        let stderr = String::from_utf8_lossy(&output.stderr);
        Err(error_text(&stderr, &format!("rclone exited with {}", output.status)))
    }
}

/// `rclone obscure -` reads the clear text from stdin, so passwords never
/// reach the command line.
async fn obscure(value: &str) -> Result<String, String> {
    let args: Vec<OsString> = vec!["obscure".into(), "-".into()];
    let mut input = value.as_bytes().to_vec();
    input.push(b'\n');
    let out = run_rclone(args, Some(&input), SHORT_TIMEOUT).await?;
    let out = out.trim().to_string();
    if out.is_empty() || !valid_option_value(&out) {
        return Err("rclone obscure returned nothing".into());
    }
    Ok(out)
}

/// One rclone remote materialised as a private config file.  The directory
/// is removed on drop.
pub struct Session {
    dir: PathBuf,
    conf: PathBuf,
    written: BTreeMap<String, String>,
}

impl Drop for Session {
    fn drop(&mut self) {
        let _ = std::fs::remove_dir_all(&self.dir);
    }
}

#[cfg(unix)]
fn create_private_dir(path: &Path) -> std::io::Result<()> {
    use std::os::unix::fs::DirBuilderExt;
    std::fs::DirBuilder::new().mode(0o700).create(path)
}

#[cfg(not(unix))]
fn create_private_dir(path: &Path) -> std::io::Result<()> {
    std::fs::create_dir(path)
}

#[cfg(unix)]
fn write_private_file(path: &Path, data: &[u8]) -> std::io::Result<()> {
    use std::io::Write;
    use std::os::unix::fs::OpenOptionsExt;
    let mut f = std::fs::OpenOptions::new().write(true).create_new(true).mode(0o600).open(path)?;
    f.write_all(data)?;
    f.sync_all()
}

#[cfg(not(unix))]
fn write_private_file(path: &Path, data: &[u8]) -> std::io::Result<()> {
    std::fs::write(path, data)
}

/// Session directories hold credentials; one left behind by a crash or kill
/// is removed once it is older than any operation can run.
fn remove_stale_sessions(tmp_root: &Path) {
    let max_age = TRANSFER_TIMEOUT + Duration::from_secs(3600);
    let Ok(entries) = std::fs::read_dir(tmp_root) else { return };
    for entry in entries.flatten() {
        let name = entry.file_name();
        if !name.to_string_lossy().starts_with("rclone-") {
            continue;
        }
        let old = entry
            .metadata()
            .and_then(|m| m.modified())
            .ok()
            .and_then(|t| t.elapsed().ok())
            .is_some_and(|age| age > max_age);
        if old {
            let _ = std::fs::remove_dir_all(entry.path());
        }
    }
}

impl Session {
    /// Validate `spec`, write the config file under `tmp_root` and, for
    /// OneDrive without a drive id, resolve the drive.
    pub async fn open(tmp_root: &Path, spec: &RemoteSpec) -> Result<Session, String> {
        validate_spec(spec)?;
        std::fs::create_dir_all(tmp_root).map_err(|e| format!("create rclone temp root: {e}"))?;
        remove_stale_sessions(tmp_root);
        let dir = tmp_root.join(format!("rclone-{}", uuid::Uuid::new_v4()));
        create_private_dir(&dir).map_err(|e| format!("create rclone temp dir: {e}"))?;
        let conf = dir.join("rclone.conf");
        let mut session = Session { dir, conf, written: BTreeMap::new() };

        let mut options = spec.options.clone();
        for key in &spec.obscure {
            if let Some(v) = options.get(key).cloned() {
                if !v.is_empty() {
                    options.insert(key.clone(), obscure(&v).await?);
                }
            }
        }
        write_private_file(&session.conf, render_config(&spec.backend, &options).as_bytes())
            .map_err(|e| format!("write rclone config: {e}"))?;
        session.written = options;

        if spec.backend == "onedrive" {
            let has_drive = spec.options.get("drive_id").is_some_and(|v| !v.is_empty())
                && spec.options.get("drive_type").is_some_and(|v| !v.is_empty());
            if !has_drive {
                session.resolve_onedrive().await?;
            }
        }
        Ok(session)
    }

    pub fn conf(&self) -> &Path {
        &self.conf
    }

    pub fn dir(&self) -> &Path {
        &self.dir
    }

    async fn resolve_onedrive(&self) -> Result<(), String> {
        let out = run_rclone(onedrive_resolve_args(&self.conf, None), None, SHORT_TIMEOUT)
            .await
            .map_err(|e| format!("looking up your OneDrive failed: {e}"))?;
        let drive_id = pick_onedrive_drive(&out).map_err(|e| format!("looking up your OneDrive failed: {e}"))?;
        let out = run_rclone(onedrive_resolve_args(&self.conf, Some(&drive_id)), None, SHORT_TIMEOUT)
            .await
            .map_err(|e| format!("selecting your OneDrive failed: {e}"))?;
        // A finished state machine answers with an empty State.
        if let Ok(done) = serde_json::from_str::<ConfigOut>(out.trim()) {
            if !done.error.is_empty() {
                return Err(format!("selecting your OneDrive failed: {}", done.error.trim()));
            }
        }
        let after = self.read_config();
        if after.get("drive_id").is_none_or(|v| v.is_empty()) || after.get("drive_type").is_none_or(|v| v.is_empty()) {
            return Err("rclone did not store the OneDrive drive id".into());
        }
        Ok(())
    }

    fn read_config(&self) -> BTreeMap<String, String> {
        std::fs::read_to_string(&self.conf).map(|t| parse_config_section(&t)).unwrap_or_default()
    }

    /// Options rclone changed while running (refreshed token, resolved drive).
    pub fn updates(&self) -> ConfigUpdates {
        diff_write_back(&self.written, &self.read_config())
    }

    pub async fn run(&self, args: Vec<OsString>, timeout: Duration) -> Result<String, String> {
        run_rclone(args, None, timeout).await
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Operations
// ─────────────────────────────────────────────────────────────────────────────

/// Result of an operation plus whatever config rclone changed meanwhile.
pub struct Outcome<T> {
    pub result: Result<T, String>,
    pub config_updates: ConfigUpdates,
}

impl<T> Outcome<T> {
    fn err(e: String) -> Self {
        Outcome { result: Err(e), config_updates: ConfigUpdates::new() }
    }
}

/// Upload `local` to `<base>/<server_id>/<file_name>`.  Returns the object path.
pub async fn upload(tmp_root: &Path, spec: &RemoteSpec, local: &Path, server_id: &str, file_name: &str) -> Outcome<String> {
    let object = match remote_object_path(&spec.base_path, server_id, file_name) {
        Ok(o) => o,
        Err(e) => return Outcome::err(e),
    };
    let session = match Session::open(tmp_root, spec).await {
        Ok(s) => s,
        Err(e) => return Outcome::err(e),
    };
    let args = copyto_args(session.conf(), &local.as_os_str().to_owned(), &remote_arg(&object).into());
    let result = session.run(args, TRANSFER_TIMEOUT).await.map(|_| object);
    Outcome { result, config_updates: session.updates() }
}

/// Download `<base>/<server_id>/<file_name>` to `local`.
pub async fn fetch(tmp_root: &Path, spec: &RemoteSpec, server_id: &str, file_name: &str, local: &Path) -> Outcome<()> {
    let object = match remote_object_path(&spec.base_path, server_id, file_name) {
        Ok(o) => o,
        Err(e) => return Outcome::err(e),
    };
    let session = match Session::open(tmp_root, spec).await {
        Ok(s) => s,
        Err(e) => return Outcome::err(e),
    };
    let args = copyto_args(session.conf(), &remote_arg(&object).into(), &local.as_os_str().to_owned());
    let result = session.run(args, TRANSFER_TIMEOUT).await.map(|_| ());
    Outcome { result, config_updates: session.updates() }
}

/// Delete `<base>/<server_id>/<file_name>`; a missing object counts as success.
pub async fn delete(tmp_root: &Path, spec: &RemoteSpec, server_id: &str, file_name: &str) -> Outcome<()> {
    let object = match remote_object_path(&spec.base_path, server_id, file_name) {
        Ok(o) => o,
        Err(e) => return Outcome::err(e),
    };
    let session = match Session::open(tmp_root, spec).await {
        Ok(s) => s,
        Err(e) => return Outcome::err(e),
    };
    let result = match session.run(deletefile_args(session.conf(), &remote_arg(&object)), DELETE_TIMEOUT).await {
        Ok(_) => Ok(()),
        Err(e) if is_not_found(&e) => Ok(()),
        Err(e) => Err(e),
    };
    Outcome { result, config_updates: session.updates() }
}

fn is_not_found(err: &str) -> bool {
    let e = err.to_ascii_lowercase();
    e.contains("object not found") || e.contains("not found") || e.contains("doesn't exist") || e.contains("no such file")
}

/// Write a small probe file to `<base>/` and delete it again: proves the
/// credentials work and the folder is writable.
pub async fn test(tmp_root: &Path, spec: &RemoteSpec) -> Outcome<()> {
    let session = match Session::open(tmp_root, spec).await {
        Ok(s) => s,
        Err(e) => return Outcome::err(e),
    };
    let probe_name = format!("talepanel-connection-test-{}.txt", uuid::Uuid::new_v4());
    let probe_local = session.dir().join("probe.txt");
    if let Err(e) = std::fs::write(&probe_local, b"TalePanel connection test - safe to delete\n") {
        return Outcome { result: Err(format!("write probe file: {e}")), config_updates: session.updates() };
    }
    let object = format!("{}/{}", spec.base_path.trim_end_matches('/'), probe_name);
    let remote = remote_arg(&object);
    let mut result = session
        .run(copyto_args(session.conf(), &probe_local.as_os_str().to_owned(), &remote.clone().into()), SHORT_TIMEOUT)
        .await
        .map(|_| ());
    if result.is_ok() {
        result = session.run(deletefile_args(session.conf(), &remote), SHORT_TIMEOUT).await.map(|_| ());
    }
    Outcome { result, config_updates: session.updates() }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn spec(backend: &str, opts: &[(&str, &str)]) -> RemoteSpec {
        RemoteSpec {
            backend: backend.into(),
            options: opts.iter().map(|(k, v)| (k.to_string(), v.to_string())).collect(),
            obscure: vec![],
            base_path: "talepanel".into(),
        }
    }

    #[test]
    fn base_path_validation() {
        for ok in ["talepanel", "TalePanel/backups", "/srv/backups", "bucket/tale panel"] {
            assert!(validate_base_path(ok).is_ok(), "{ok}");
        }
        for bad in ["", "/", "..", "a/../b", "./a", "a//b", "a/", "-a", "a/--config", "a\nb", "a\\b", "x:y", " a", "a/b "] {
            assert!(validate_base_path(bad).is_err(), "{bad:?}");
        }
        assert!(validate_base_path(&"a".repeat(513)).is_err());
    }

    #[test]
    fn name_component_validation() {
        assert!(valid_name_component("0b6f0c2e-1d2c-4c39-9b5e-3f1f3c2d1a00"));
        assert!(valid_name_component("0b6f0c2e-1d2c-4c39-9b5e-3f1f3c2d1a00.zip"));
        for bad in ["", "..", "../x", "a/b", "-rf", ".hidden", "a b", "a..b"] {
            assert!(!valid_name_component(bad), "{bad:?}");
        }
    }

    #[test]
    fn remote_object_path_joins_and_validates() {
        assert_eq!(remote_object_path("tp/backups", "srv", "b.zip").unwrap(), "tp/backups/srv/b.zip");
        assert_eq!(remote_object_path("/abs", "srv", "b.zip").unwrap(), "/abs/srv/b.zip");
        assert!(remote_object_path("../x", "srv", "b.zip").is_err());
        assert!(remote_object_path("ok", "../srv", "b.zip").is_err());
        assert!(remote_object_path("ok", "srv", "-b.zip").is_err());
        assert_eq!(remote_arg("a/b"), "tp:a/b");
    }

    #[test]
    fn spec_validation_rejects_dangerous_options() {
        assert!(validate_spec(&spec("onedrive", &[("token", "{}")])).is_ok());
        assert!(validate_spec(&spec("local", &[])).is_err());
        assert!(validate_spec(&spec("crypt", &[])).is_err());
        for key in ["ssh", "bearer_token_command", "password_command", "key_file", "known_hosts_file",
                    "service_account_file", "type", "Token", "1abc", "a-b", "config_is_local", ""] {
            assert!(validate_spec(&spec("sftp", &[(key, "x")])).is_err(), "{key:?}");
        }
        assert!(validate_spec(&spec("sftp", &[("host", "h\n[evil]\ntype = local")])).is_err());
        assert!(validate_spec(&spec("sftp", &[("host", "h\rx")])).is_err());
        let mut s = spec("sftp", &[("pass", "x")]);
        s.obscure = vec!["ssh".into()];
        assert!(validate_spec(&s).is_err());
        let mut s = spec("sftp", &[]);
        s.base_path = "../etc".into();
        assert!(validate_spec(&s).is_err());
    }

    #[test]
    fn config_render_and_parse_roundtrip() {
        let opts: BTreeMap<String, String> = [
            ("token".to_string(), r#"{"access_token":"a=b","expiry":"x"}"#.to_string()),
            ("drive_type".to_string(), "personal".to_string()),
        ]
        .into_iter()
        .collect();
        let text = render_config("onedrive", &opts);
        assert!(text.starts_with("[tp]\ntype = onedrive\n"));
        let parsed = parse_config_section(&format!("[other]\ntoken = nope\n\n{text}"));
        assert_eq!(parsed.get("type").map(String::as_str), Some("onedrive"));
        assert_eq!(parsed.get("token"), opts.get("token"));
    }

    #[test]
    fn write_back_only_reports_changed_allowed_keys() {
        let before: BTreeMap<String, String> =
            [("token", "old"), ("user", "u")].iter().map(|(k, v)| (k.to_string(), v.to_string())).collect();
        let after: BTreeMap<String, String> = [("token", "new"), ("user", "changed"), ("drive_id", "abc"), ("drive_type", "")]
            .iter()
            .map(|(k, v)| (k.to_string(), v.to_string()))
            .collect();
        let d = diff_write_back(&before, &after);
        assert_eq!(d.len(), 2);
        assert_eq!(d.get("token").map(String::as_str), Some("new"));
        assert_eq!(d.get("drive_id").map(String::as_str), Some("abc"));
        assert!(diff_write_back(&before, &before).is_empty());
    }

    fn strs(args: &[OsString]) -> Vec<String> {
        args.iter().map(|a| a.to_string_lossy().to_string()).collect()
    }

    #[test]
    fn argument_building_keeps_secrets_off_the_command_line() {
        let conf = Path::new("/tmp/x/rclone.conf");
        let up = strs(&copyto_args(conf, &"/srv/b.zip".into(), &"tp:base/s/b.zip".into()));
        assert_eq!(up[0], "copyto");
        assert_eq!(&up[1..3], &["--config", "/tmp/x/rclone.conf"]);
        let dd = up.iter().position(|a| a == "--").unwrap();
        assert_eq!(&up[dd + 1..], &["/srv/b.zip", "tp:base/s/b.zip"]);

        let del = strs(&deletefile_args(conf, "tp:base/s/b.zip"));
        assert_eq!(del, vec!["deletefile", "--config", "/tmp/x/rclone.conf", "--", "tp:base/s/b.zip"]);

        let od = strs(&onedrive_resolve_args(conf, None));
        assert_eq!(
            od,
            vec!["config", "update", "--config", "/tmp/x/rclone.conf", "--non-interactive", "--", "tp",
                 "config_refresh_token=false", "config_type=onedrive"]
        );
        let od = strs(&onedrive_resolve_args(conf, Some("b!abc")));
        assert!(od.ends_with(&["config_driveid=b!abc".to_string(), "config_drive_ok=true".to_string()]));
        for a in up.iter().chain(del.iter()) {
            assert!(!a.contains("token"), "{a}");
        }
    }

    #[test]
    fn onedrive_drive_selection() {
        let json = r#"{
            "State": "driveid_final,,",
            "Option": {"Name": "config_driveid", "Help": "Select drive you want to use",
              "Examples": [
                {"Value": "lib1", "Help": "Documents (documentLibrary)"},
                {"Value": "b!Xy_z-1", "Help": "OneDrive (business)"}
              ]},
            "Error": ""
        }"#;
        assert_eq!(pick_onedrive_drive(json).unwrap(), "b!Xy_z-1");
        let only = r#"{"State":"x","Option":{"Name":"config_driveid","Examples":[{"Value":"abc123","Help":"OneDrive (personal)"}]},"Error":""}"#;
        assert_eq!(pick_onedrive_drive(only).unwrap(), "abc123");
        let err = r#"{"State":"driveid","Option":{"Name":"config_driveid_fixed"},"Error":"Failed to query available drives: 401"}"#;
        assert!(pick_onedrive_drive(err).unwrap_err().contains("401"));
        let bad_id = r#"{"State":"x","Option":{"Name":"config_driveid","Examples":[{"Value":"--config=/x","Help":"x"}]},"Error":""}"#;
        assert!(pick_onedrive_drive(bad_id).is_err());
        assert!(pick_onedrive_drive("not json").is_err());
    }

    /// End-to-end against a real rclone and a throwaway WebDAV server:
    ///
    ///   rclone serve webdav /tmp/dav --addr 127.0.0.1:18080 --user tp --pass secret
    ///   TP_E2E_WEBDAV_URL=http://127.0.0.1:18080/ TP_E2E_WEBDAV_USER=tp \
    ///   TP_E2E_WEBDAV_PASS=secret cargo test rclone::tests::e2e -- --ignored
    ///
    /// Exercises `rclone obscure -`, the private config file, copyto in both
    /// directions, deletefile (and its idempotence) and the connection test.
    #[tokio::test]
    #[ignore]
    async fn e2e_webdav_roundtrip() {
        let url = std::env::var("TP_E2E_WEBDAV_URL").expect("TP_E2E_WEBDAV_URL");
        let user = std::env::var("TP_E2E_WEBDAV_USER").expect("TP_E2E_WEBDAV_USER");
        let pass = std::env::var("TP_E2E_WEBDAV_PASS").expect("TP_E2E_WEBDAV_PASS");
        let work = std::env::temp_dir().join(format!("tp-rclone-e2e-{}", uuid::Uuid::new_v4()));
        std::fs::create_dir_all(&work).unwrap();
        let tmp_root = work.join("tmp");
        let spec = RemoteSpec {
            backend: "webdav".into(),
            options: [("url", url.as_str()), ("vendor", "other"), ("user", user.as_str()), ("pass", pass.as_str())]
                .iter()
                .map(|(k, v)| (k.to_string(), v.to_string()))
                .collect(),
            obscure: vec!["pass".into()],
            base_path: "TalePanel/backups".into(),
        };

        let t = test(&tmp_root, &spec).await;
        assert!(t.result.is_ok(), "test: {:?}", t.result);

        let local = work.join("archive.zip");
        std::fs::write(&local, b"PK fake archive bytes").unwrap();
        let server = "0b6f0c2e-1d2c-4c39-9b5e-3f1f3c2d1a00";
        let up = upload(&tmp_root, &spec, &local, server, "b1.zip").await;
        assert_eq!(up.result.as_deref(), Ok("TalePanel/backups/0b6f0c2e-1d2c-4c39-9b5e-3f1f3c2d1a00/b1.zip"));
        assert!(up.config_updates.is_empty());

        let back = work.join("fetched.zip");
        let f = fetch(&tmp_root, &spec, server, "b1.zip", &back).await;
        assert!(f.result.is_ok(), "fetch: {:?}", f.result);
        assert_eq!(std::fs::read(&back).unwrap(), b"PK fake archive bytes");

        let d = delete(&tmp_root, &spec, server, "b1.zip").await;
        assert!(d.result.is_ok(), "delete: {:?}", d.result);
        let d2 = delete(&tmp_root, &spec, server, "b1.zip").await;
        assert!(d2.result.is_ok(), "second delete must be idempotent: {:?}", d2.result);
        let gone = fetch(&tmp_root, &spec, server, "b1.zip", &work.join("gone.zip")).await;
        assert!(gone.result.is_err());

        // Wrong password: rclone's error text comes back.
        let mut bad = spec.clone();
        bad.options.insert("pass".into(), "wrong".into());
        let e = test(&tmp_root, &bad).await.result.unwrap_err();
        assert!(!e.is_empty());
        eprintln!("wrong-password error text: {e}");

        // No session directory (with credentials) is left behind.
        let left: Vec<_> = std::fs::read_dir(&tmp_root).unwrap().flatten().collect();
        assert!(left.is_empty(), "leftover session dirs: {left:?}");
        let _ = std::fs::remove_dir_all(&work);
    }

    #[test]
    fn error_text_prefers_error_lines_and_truncates() {
        let stderr = "2026/10/23 NOTICE: something\n2026/10/23 ERROR : b.zip: Failed to copy: 401 Unauthorized\n";
        assert_eq!(error_text(stderr, "fb"), "2026/10/23 ERROR : b.zip: Failed to copy: 401 Unauthorized");
        assert_eq!(error_text("", "fallback"), "fallback");
        let retries = "2026/10/23 23:20:31 ERROR : Attempt 1/3 failed with 1 errors and: read metadata failed: 401 Unauthorized\n\
                       2026/10/23 23:20:31 ERROR : Attempt 2/3 failed with 1 errors and: read metadata failed: 401 Unauthorized\n\
                       2026/10/23 23:20:31 NOTICE: Failed to copyto: read metadata failed: 401 Unauthorized\n";
        assert_eq!(error_text(retries, ""), "Failed to copyto: read metadata failed: 401 Unauthorized");
        let no_summary = "2026/10/23 23:20:31 ERROR : x: boom\n2026/10/23 23:20:32 ERROR : x: boom\n";
        assert_eq!(error_text(no_summary, ""), "x: boom");
        assert!(error_text(&"ERROR x".repeat(1000), "").len() <= MAX_ERROR_LEN + 3);
    }
}
