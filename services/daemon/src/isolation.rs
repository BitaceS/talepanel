//! Per-server process isolation (roadmap "Now" #9, step 1).
//!
//! Without isolation every game server runs as the daemon's user (root in the
//! container) and can read the node token (`/proc/1/environ`), the node's TLS
//! key, cloud backup credentials and every other server's files and game
//! session. With isolation:
//!
//! * every server gets its own unprivileged UID/GID, `server_uid_base + n`.
//!   The UID is recorded in the owner of the server's directory; a high-water
//!   mark (`{data_root}/server-uids.next`) keeps UIDs from being handed out
//!   again after a server is deleted;
//! * the server's directory (`{data_root}/servers/<id>`) belongs to that UID
//!   with mode 0700, so servers cannot see each other;
//! * the JVM is spawned with that UID/GID, no supplementary groups,
//!   `no_new_privs`, umask 007 and an allowlisted environment (see
//!   `process::hytale`);
//! * the node's own files are root-only: `data_root` and `servers/` are 0711,
//!   everything else in `data_root`, the PKI dir and the config file are
//!   owner-only, and the daemon runs with umask 077 so files it creates later
//!   are private too;
//! * the daemon does its file work in a server's directory with that server's
//!   file permissions (`userfs`), so a server racing symlinks into place
//!   cannot make root read or write outside its directory;
//! * processes a server leaves behind are killed when it stops, before it
//!   starts and before it is deleted, so nothing of a server outlives it.
//!
//! Files that root put into a server directory (provisioning, upgrades from
//! a daemon without isolation) are handed over by an ownership walk. It runs
//! as root inside a directory the server controls, so it works on file
//! descriptors only: directories are opened `O_NOFOLLOW`, every other entry
//! through an `O_PATH | O_NOFOLLOW` descriptor, the decision is made on that
//! descriptor's `fstat`, and the owner is changed on the same descriptor.
//! Non-directories of another owner with more than one hard link are left
//! alone, so a hard link to a foreign file cannot be used to take it over.

use std::path::{Path, PathBuf};
use std::sync::Arc;

use anyhow::{bail, Context, Result};
use dashmap::DashMap;
use tracing::{error, info, warn};

use crate::config::Config;

/// Number of UIDs reserved for game servers, starting at `server_uid_base`.
pub const UID_SPAN: u32 = 65_536;

/// Directory nesting the ownership walk follows; deeper entries are skipped.
const MAX_DEPTH: usize = 128;

/// High-water mark of handed-out UIDs, in `data_root`.
const HIGH_WATER_FILE: &str = "server-uids.next";

/// The unprivileged identity a game server runs under.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub struct ServerIdentity {
    pub uid: u32,
    pub gid: u32,
}

impl ServerIdentity {
    /// Account name used in `/etc/passwd` and `/etc/group`.
    pub fn account_name(&self) -> String {
        format!("tpsrv{}", self.uid)
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Mode {
    Off,
    Uid,
}

/// Parses `server_isolation`. `None` means `auto`.
pub fn parse_mode(value: &str) -> Result<Option<Mode>> {
    match value.trim().to_ascii_lowercase().as_str() {
        "" | "auto" => Ok(None),
        "uid" | "on" | "1" | "true" | "yes" => Ok(Some(Mode::Uid)),
        "off" | "0" | "false" | "no" | "none" => Ok(Some(Mode::Off)),
        other => bail!("server_isolation must be auto, uid or off, not {other:?}"),
    }
}

/// Checks that the UID range neither starts among regular users nor covers
/// `nobody` (65534) or overflows.
pub fn validate_uid_base(base: u32) -> Result<()> {
    if base < 10_000 {
        bail!("server_uid_base must be at least 10000 (got {base}) to stay clear of regular users");
    }
    let Some(end) = base.checked_add(UID_SPAN) else {
        bail!("server_uid_base {base} is too large");
    };
    if (base..end).contains(&65_534) {
        bail!("server_uid_base {base}: the range must not contain 65534 (nobody)");
    }
    if end > i32::MAX as u32 {
        bail!("server_uid_base {base} is too large");
    }
    Ok(())
}

/// Picks the UID for a server directory currently owned by `own_uid`.
/// Keeps a UID it already has (unless another directory has it too), else
/// hands out the next never-used UID. Only when the range is exhausted is a
/// free UID reused; its leftover processes are killed before every start.
/// Returns the UID and whether it is newly handed out.
fn choose_uid(
    own_uid: u32,
    used: &std::collections::HashSet<u32>,
    base: u32,
    high_water: Option<u32>,
) -> Option<(u32, bool)> {
    let in_range = |u: u32| u >= base && u - base < UID_SPAN;
    if in_range(own_uid) && !used.contains(&own_uid) {
        return Some((own_uid, false));
    }
    let mut next = high_water.unwrap_or(base).max(base);
    if let Some(max_used) = used.iter().copied().filter(|u| in_range(*u)).max() {
        next = next.max(max_used + 1);
    }
    if in_range(next) {
        return Some((next, true));
    }
    (base..base + UID_SPAN)
        .find(|u| !used.contains(u))
        .map(|u| (u, true))
}

/// Shared isolation state.
pub struct Isolation {
    mode: Mode,
    data_root: PathBuf,
    servers_dir: PathBuf,
    uid_base: u32,
    /// Where `passwd` and `group` live (`/etc`; a temp dir in tests). `None`
    /// skips account entries.
    etc_dir: Option<PathBuf>,
    /// Serialises UID selection and the high-water file.
    alloc: Arc<std::sync::Mutex<()>>,
    /// Serialises `/etc/passwd` and `/etc/group` rewrites.
    accounts: Arc<std::sync::Mutex<()>>,
    /// Per-server lock around prepare, repair and kill_leftovers, so a kill
    /// of leftovers can never overlap a start of the same server. Other
    /// servers are not held up.
    locks: DashMap<String, Arc<tokio::sync::Mutex<()>>>,
}

impl Isolation {
    /// Isolation switched off, for tests.
    #[cfg(test)]
    pub fn disabled(servers_dir: impl Into<PathBuf>) -> Self {
        let servers_dir = servers_dir.into();
        Self {
            mode: Mode::Off,
            data_root: servers_dir
                .parent()
                .map(Path::to_path_buf)
                .unwrap_or_default(),
            servers_dir,
            uid_base: crate::config::DaemonConfig::default_server_uid_base(),
            etc_dir: None,
            alloc: Default::default(),
            accounts: Default::default(),
            locks: DashMap::new(),
        }
    }

    /// Resolves the configured mode. `auto` turns isolation on when the
    /// daemon is root on Linux and the data directory keeps ownership
    /// changes. `uid` fails when that is not the case, so an operator who
    /// asked for isolation never silently runs without it.
    pub fn from_config(config: &Config) -> Result<Self> {
        let data_root = PathBuf::from(config.daemon.data_root.trim_end_matches('/'));
        let servers_dir = data_root.join("servers");
        let requested = parse_mode(&config.daemon.server_isolation)?;
        let base = config.daemon.server_uid_base;
        let mut mode = match requested {
            Some(Mode::Off) => Mode::Off,
            Some(Mode::Uid) if !platform_supported() => bail!(
                "server_isolation=uid needs the daemon to run as root on Linux; \
                 run it as root or set TALEDAEMON_SERVER_ISOLATION=off"
            ),
            Some(Mode::Uid) => Mode::Uid,
            None if platform_supported() => Mode::Uid,
            None => Mode::Off,
        };
        if mode == Mode::Uid {
            validate_uid_base(base)?;
            // Some file systems (Docker Desktop bind mounts, NFS with
            // root_squash, FUSE) ignore or refuse chown. Isolation would then
            // be on in name only, or refuse every start.
            if let Err(e) = sys::probe_ownership(&servers_dir, base + UID_SPAN - 1) {
                if requested == Some(Mode::Uid) {
                    return Err(e.context(format!(
                        "server_isolation=uid, but {} does not keep file ownership",
                        servers_dir.display()
                    )));
                }
                error!(
                    servers_dir = %servers_dir.display(),
                    error = %format!("{e:#}"),
                    "Per-server isolation is OFF: the data directory does not keep file ownership \
                     (Docker Desktop bind mount, NFS root_squash or similar). Use a local Linux \
                     file system for the data root."
                );
                mode = Mode::Off;
            }
        }
        Ok(Self {
            mode,
            data_root,
            servers_dir,
            uid_base: base,
            etc_dir: Some(PathBuf::from("/etc")),
            alloc: Default::default(),
            accounts: Default::default(),
            locks: DashMap::new(),
        })
    }

    pub fn is_enabled(&self) -> bool {
        self.mode == Mode::Uid
    }

    /// Prepares a server for a start: kills what an earlier run left behind,
    /// picks its UID, makes sure the account exists, hands its directory to
    /// it. `Ok(None)` when isolation is off. An error means the server must
    /// not start.
    pub async fn prepare(&self, server_id: &str) -> Result<Option<ServerIdentity>> {
        if !self.is_enabled() {
            return Ok(None);
        }
        let lock = self.server_lock(server_id);
        let _held = lock.lock().await;
        let Some((id, _)) = self.claim(server_id, false).await? else {
            return Ok(None);
        };
        let killed = kill_processes(id).await?;
        if killed > 0 {
            warn!(
                server_id,
                uid = id.uid,
                killed,
                "Killed processes left over from an earlier run"
            );
        }
        self.walk(server_id, id).await?;
        if let Some(etc) = self.etc_dir.clone() {
            let home = self.servers_dir.join(server_id);
            let accounts = Arc::clone(&self.accounts);
            let result = tokio::task::spawn_blocking(move || {
                let _lock = accounts.lock().unwrap_or_else(|e| e.into_inner());
                ensure_account(&etc, id, &home)
            })
            .await
            .context("account task panicked")?;
            // Without an entry the JVM still runs; it only lacks a user name
            // (user.name "?"). Not worth refusing a start.
            if let Err(e) = result {
                warn!(server_id, uid = id.uid, error = %format!("{e:#}"),
                    "Could not add the server's account to /etc/passwd");
            }
        }
        Ok(Some(id))
    }

    /// The identity to do file work in a server's directory with. Creates
    /// the directory when it is missing and picks a UID for it, but does not
    /// walk the tree. `Ok(None)` when isolation is off.
    pub async fn identity_for(&self, server_id: &str) -> Result<Option<ServerIdentity>> {
        if !self.is_enabled() {
            return Ok(None);
        }
        let lock = self.server_lock(server_id);
        let _held = lock.lock().await;
        let Some((id, changed)) = self.claim(server_id, true).await? else {
            return Ok(None);
        };
        // First contact after an upgrade (or with a directory root created):
        // hand the contents over too, so the server's permissions can work
        // with them.
        if changed {
            self.walk(server_id, id).await?;
        }
        Ok(Some(id))
    }

    /// Hands everything root left in the server's directory to the server.
    /// For provisioning (the downloader runs as root while the server is
    /// stopped) and the startup sweep. No-op when isolation is off or the
    /// directory does not exist.
    pub async fn repair(&self, server_id: &str) -> Result<()> {
        if !self.is_enabled() {
            return Ok(());
        }
        let lock = self.server_lock(server_id);
        let _held = lock.lock().await;
        if let Some((id, _)) = self.claim(server_id, false).await? {
            self.walk(server_id, id).await?;
        }
        Ok(())
    }

    /// Kills every process of the server's UID. For stopped servers and
    /// before a server directory is deleted. No-op when isolation is off or
    /// the directory is not owned by a server UID.
    pub async fn kill_leftovers(&self, server_id: &str) -> Result<usize> {
        if !self.is_enabled() || !is_canonical_uuid(server_id) {
            return Ok(0);
        }
        let lock = self.server_lock(server_id);
        let _held = lock.lock().await;
        let dir = self.servers_dir.join(server_id);
        let base = self.uid_base;
        let owner = tokio::task::spawn_blocking(move || {
            use std::os::unix::fs::MetadataExt;
            std::fs::symlink_metadata(&dir)
                .ok()
                .map(|m| (m.uid(), m.gid()))
        })
        .await
        .context("stat task panicked")?;
        match owner {
            Some((uid, gid)) if uid >= base && uid - base < UID_SPAN => {
                kill_processes(ServerIdentity { uid, gid }).await
            }
            _ => Ok(0),
        }
    }

    /// Hands every existing server directory to its UID. Run once at startup
    /// so servers that were never started under isolation (stopped ones, or
    /// all of them right after an upgrade) are closed to the others too.
    pub async fn claim_all(&self) {
        if !self.is_enabled() {
            return;
        }
        let ids: Vec<String> = match std::fs::read_dir(&self.servers_dir) {
            Ok(entries) => entries
                .flatten()
                .filter(|e| e.file_type().map(|t| t.is_dir()).unwrap_or(false))
                .filter_map(|e| e.file_name().into_string().ok())
                .filter(|name| is_canonical_uuid(name))
                .collect(),
            Err(e) => {
                warn!(servers_dir = %self.servers_dir.display(), error = %e,
                    "Could not list server directories");
                return;
            }
        };
        let mut claimed = 0usize;
        for id in &ids {
            match self.repair(id).await {
                Ok(()) => claimed += 1,
                Err(e) => warn!(server_id = %id, error = %format!("{e:#}"),
                    "Could not hand the server directory to its user"),
            }
        }
        info!(
            claimed,
            total = ids.len(),
            "Server directories handed to their own users"
        );
    }

    /// Picks the UID and fixes the owner and mode of the directory itself.
    /// The flag says whether the directory changed hands (its contents then
    /// still need the walk).
    async fn claim(&self, server_id: &str, create: bool) -> Result<Option<(ServerIdentity, bool)>> {
        if !is_canonical_uuid(server_id) {
            bail!("refusing to isolate {server_id:?}: not a server id");
        }
        let servers_dir = self.servers_dir.clone();
        let high_water = self.data_root.join(HIGH_WATER_FILE);
        let server_id = server_id.to_string();
        let base = self.uid_base;
        let alloc = Arc::clone(&self.alloc);
        tokio::task::spawn_blocking(move || {
            let _guard = alloc.lock().unwrap_or_else(|e| e.into_inner());
            sys::claim_server_dir(&servers_dir, &server_id, base, &high_water, create)
        })
        .await
        .context("isolation task panicked")?
    }

    fn server_lock(&self, server_id: &str) -> Arc<tokio::sync::Mutex<()>> {
        Arc::clone(self.locks.entry(server_id.to_string()).or_default().value())
    }

    /// The ownership walk. Callers hold the server's lock.
    async fn walk(&self, server_id: &str, id: ServerIdentity) -> Result<()> {
        let servers_dir = self.servers_dir.clone();
        let sid = server_id.to_string();
        let skipped =
            tokio::task::spawn_blocking(move || sys::repair_server_dir(&servers_dir, &sid, id))
                .await
                .context("ownership walk panicked")??;
        if skipped > 0 {
            warn!(
                server_id,
                skipped,
                "Left entries in the server directory unchanged (foreign hard links or nesting \
                 deeper than {MAX_DEPTH})"
            );
        }
        Ok(())
    }

    /// Makes the node's own files root-only and sets the daemon's umask to
    /// 077, so files it creates later are private too. Called once at startup
    /// when isolation is on. Failures are logged, not fatal: a node that
    /// cannot tighten a permission is no worse off than before.
    pub fn secure_node_layout(&self, extra_private: &[&Path]) {
        if !self.is_enabled() {
            return;
        }
        // SAFETY: umask has no preconditions. Process-wide on purpose.
        unsafe { libc::umask(0o077) };
        // File work done with a server's fsuid/fsgid still carries the
        // daemon's supplementary groups; without them a group-readable root
        // file reached through a server's symlink stays closed. glibc applies
        // setgroups to every thread of the process.
        // SAFETY: a null list of length 0 is valid.
        if unsafe { libc::setgroups(0, std::ptr::null()) } != 0 {
            warn!(error = %std::io::Error::last_os_error(), "Could not drop supplementary groups");
        }
        for (path, mode) in node_layout_modes(&self.data_root, &self.servers_dir, extra_private) {
            if let Err(e) = set_mode_nofollow(&path, mode) {
                warn!(path = %path.display(), error = %e, "Could not tighten permissions");
            }
        }
        // Hytale derives the key of its credential store from the machine-id;
        // the server has to be able to read it.
        let _ = set_mode_nofollow(Path::new("/etc/machine-id"), 0o644);
        if let Ok(v) = std::fs::read_to_string("/proc/sys/fs/protected_hardlinks") {
            if v.trim() == "0" {
                warn!(
                    "fs.protected_hardlinks is 0 on this host; set it to 1 so servers cannot \
                       hard-link files they do not own"
                );
            }
        }
        info!(
            servers_dir = %self.servers_dir.display(),
            uid_base = self.uid_base,
            "Per-server isolation on: every game server runs under its own UID"
        );
    }
}

fn is_canonical_uuid(id: &str) -> bool {
    id.len() == 36 && uuid::Uuid::try_parse(id).is_ok()
}

/// The permission changes [`Isolation::secure_node_layout`] makes:
/// `data_root` and `servers/` 0711 (enter, but not list), every other entry of
/// `data_root` and each `extra_private` path owner-only.
fn node_layout_modes(
    data_root: &Path,
    servers_dir: &Path,
    extra_private: &[&Path],
) -> Vec<(PathBuf, u32)> {
    let mut out = vec![(data_root.to_path_buf(), 0o711)];
    if let Ok(entries) = std::fs::read_dir(data_root) {
        for entry in entries.flatten() {
            let path = entry.path();
            if path == servers_dir {
                out.push((path, 0o711));
                continue;
            }
            match entry.file_type() {
                Ok(t) if t.is_dir() => out.push((path, 0o700)),
                Ok(t) if t.is_file() => out.push((path, 0o600)),
                _ => {}
            }
        }
    }
    for p in extra_private {
        if p.is_dir() {
            out.push((p.to_path_buf(), 0o700));
        } else if p.is_file() {
            out.push((p.to_path_buf(), 0o600));
        }
    }
    out
}

/// chmod without following a symlink at `path` (symlinks are skipped).
fn set_mode_nofollow(path: &Path, mode: u32) -> std::io::Result<()> {
    use std::os::unix::fs::PermissionsExt;
    let md = std::fs::symlink_metadata(path)?;
    if md.file_type().is_symlink() {
        return Ok(());
    }
    if md.permissions().mode() & 0o7777 == mode {
        return Ok(());
    }
    std::fs::set_permissions(path, std::fs::Permissions::from_mode(mode))
}

fn platform_supported() -> bool {
    #[cfg(target_os = "linux")]
    {
        // SAFETY: geteuid has no preconditions and cannot fail.
        unsafe { libc::geteuid() == 0 }
    }
    #[cfg(not(target_os = "linux"))]
    {
        false
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Leftover processes
// ─────────────────────────────────────────────────────────────────────────────

/// Kills every process running as `id.uid` and returns how many there were.
/// A helper process switches to the UID and calls `kill(-1, SIGKILL)`, which
/// takes all of them at once (a fork loop cannot outrun it), repeated until
/// none are left.
async fn kill_processes(id: ServerIdentity) -> Result<usize> {
    if id.uid == 0 {
        bail!("refusing to kill the processes of uid 0");
    }
    tokio::task::spawn_blocking(move || sys::kill_uid(id))
        .await
        .context("kill task panicked")?
}

// ─────────────────────────────────────────────────────────────────────────────
// /etc/passwd and /etc/group
// ─────────────────────────────────────────────────────────────────────────────

/// Adds (or updates) `tpsrv<uid>` in `{etc}/passwd` and `{etc}/group`, so the
/// JVM sees a user name and a home directory. Callers hold the accounts lock.
fn ensure_account(etc: &Path, id: ServerIdentity, home: &Path) -> Result<()> {
    let name = id.account_name();
    let passwd_line = format!(
        "{name}:x:{}:{}:TalePanel game server:{}:/usr/sbin/nologin",
        id.uid,
        id.gid,
        home.display()
    );
    let group_line = format!("{name}:x:{}:", id.gid);
    upsert_file(&etc.join("passwd"), &name, id.uid, &passwd_line)?;
    upsert_file(&etc.join("group"), &name, id.gid, &group_line)?;
    Ok(())
}

fn upsert_file(path: &Path, name: &str, id: u32, line: &str) -> Result<()> {
    use std::io::Write;
    use std::os::unix::fs::{OpenOptionsExt, PermissionsExt};
    let current =
        std::fs::read_to_string(path).with_context(|| format!("reading {}", path.display()))?;
    let Some(updated) = upsert_entry(&current, name, id, line)? else {
        return Ok(());
    };
    let tmp = path.with_file_name(format!(
        ".{}.taledaemon-{}",
        path.file_name().and_then(|n| n.to_str()).unwrap_or("db"),
        uuid::Uuid::new_v4().simple()
    ));
    let write = || -> std::io::Result<()> {
        let mut f = std::fs::OpenOptions::new()
            .write(true)
            .create_new(true)
            .mode(0o644)
            .open(&tmp)?;
        f.write_all(updated.as_bytes())?;
        // The umask may have narrowed the create mode.
        f.set_permissions(std::fs::Permissions::from_mode(0o644))?;
        f.sync_all()?;
        std::fs::rename(&tmp, path)
    };
    if let Err(e) = write() {
        let _ = std::fs::remove_file(&tmp);
        return Err(e).with_context(|| format!("replacing {}", path.display()));
    }
    Ok(())
}

/// Returns the new file content when `name` must be added or its line
/// changed, `None` when it is already right. An entry of someone else with
/// the same numeric id is an error: two accounts must never share it.
fn upsert_entry(content: &str, name: &str, id: u32, line: &str) -> Result<Option<String>> {
    let mut out: Vec<&str> = Vec::new();
    let mut found = false;
    let mut changed = false;
    for existing in content.lines() {
        let mut fields = existing.split(':');
        let entry_name = fields.next().unwrap_or("");
        let entry_id = fields.nth(1).and_then(|v| v.parse::<u32>().ok());
        if entry_name == name {
            if found {
                changed = true; // drop duplicates
                continue;
            }
            found = true;
            if existing != line {
                changed = true;
                out.push(line);
                continue;
            }
        } else if entry_id == Some(id) {
            bail!("id {id} already belongs to {entry_name:?}; choose another server_uid_base");
        }
        out.push(existing);
    }
    if !found {
        out.push(line);
        changed = true;
    }
    if !changed {
        return Ok(None);
    }
    let mut s = out.join("\n");
    s.push('\n');
    Ok(Some(s))
}

// ─────────────────────────────────────────────────────────────────────────────
// File-descriptor based directory work (Linux)
// ─────────────────────────────────────────────────────────────────────────────

#[cfg(target_os = "linux")]
mod sys {
    use super::{choose_uid, ServerIdentity, MAX_DEPTH};
    use anyhow::{bail, Context, Result};
    use std::collections::HashSet;
    use std::ffi::{CStr, CString};
    use std::io;
    use std::os::fd::{AsRawFd, FromRawFd, OwnedFd};
    use std::os::unix::ffi::OsStrExt;
    use std::path::Path;

    fn cvt(r: libc::c_int) -> io::Result<libc::c_int> {
        if r < 0 {
            Err(io::Error::last_os_error())
        } else {
            Ok(r)
        }
    }

    const DIR_FLAGS: libc::c_int =
        libc::O_RDONLY | libc::O_DIRECTORY | libc::O_NOFOLLOW | libc::O_CLOEXEC;
    const PATH_FLAGS: libc::c_int = libc::O_PATH | libc::O_NOFOLLOW | libc::O_CLOEXEC;

    fn open_dir(path: &Path) -> io::Result<OwnedFd> {
        let c = CString::new(path.as_os_str().as_bytes())
            .map_err(|_| io::Error::from(io::ErrorKind::InvalidInput))?;
        // SAFETY: valid NUL-terminated path; the fd is owned by the result.
        let fd = cvt(unsafe { libc::open(c.as_ptr(), DIR_FLAGS) })?;
        Ok(unsafe { OwnedFd::from_raw_fd(fd) })
    }

    fn open_at(dir: &OwnedFd, name: &CStr, flags: libc::c_int) -> io::Result<OwnedFd> {
        // SAFETY: dir is an open directory fd, name a single component.
        let fd = cvt(unsafe { libc::openat(dir.as_raw_fd(), name.as_ptr(), flags) })?;
        Ok(unsafe { OwnedFd::from_raw_fd(fd) })
    }

    fn stat_at(dir: &OwnedFd, name: &CStr) -> io::Result<libc::stat> {
        let mut st: libc::stat = unsafe { std::mem::zeroed() };
        // SAFETY: st is a valid out-pointer; AT_SYMLINK_NOFOLLOW stats the
        // entry itself.
        cvt(unsafe {
            libc::fstatat(
                dir.as_raw_fd(),
                name.as_ptr(),
                &mut st,
                libc::AT_SYMLINK_NOFOLLOW,
            )
        })?;
        Ok(st)
    }

    fn fstat(fd: &OwnedFd) -> io::Result<libc::stat> {
        let mut st: libc::stat = unsafe { std::mem::zeroed() };
        // SAFETY: valid fd and out-pointer.
        cvt(unsafe { libc::fstat(fd.as_raw_fd(), &mut st) })?;
        Ok(st)
    }

    /// Changes the owner of whatever `fd` refers to. On an `O_PATH` fd of a
    /// symlink, AT_EMPTY_PATH changes the link itself, never its target.
    fn chown_fd(fd: &OwnedFd, id: ServerIdentity) -> io::Result<()> {
        // SAFETY: valid fd and an empty, NUL-terminated path.
        cvt(unsafe {
            libc::fchownat(
                fd.as_raw_fd(),
                c"".as_ptr(),
                id.uid,
                id.gid,
                libc::AT_EMPTY_PATH | libc::AT_SYMLINK_NOFOLLOW,
            )
        })
        .map(|_| ())
    }

    fn fchmod(fd: &OwnedFd, mode: libc::mode_t) -> io::Result<()> {
        // SAFETY: valid fd.
        cvt(unsafe { libc::fchmod(fd.as_raw_fd(), mode) }).map(|_| ())
    }

    /// Calls `f` for every name in a directory except `.` and `..`, without
    /// collecting them first (a server may create millions of entries).
    fn for_each_entry(dir: &OwnedFd, mut f: impl FnMut(&CStr) -> Result<()>) -> Result<()> {
        // fdopendir takes ownership of the fd it gets; hand it a duplicate.
        // SAFETY: dup of a valid fd.
        let dup = cvt(unsafe { libc::fcntl(dir.as_raw_fd(), libc::F_DUPFD_CLOEXEC, 0) })?;
        // SAFETY: dup is a valid directory fd; on success the DIR owns it.
        let dirp = unsafe { libc::fdopendir(dup) };
        if dirp.is_null() {
            let e = io::Error::last_os_error();
            unsafe { libc::close(dup) };
            return Err(e.into());
        }
        struct Closer(*mut libc::DIR);
        impl Drop for Closer {
            fn drop(&mut self) {
                // SAFETY: closed exactly once (also closes the dup).
                unsafe { libc::closedir(self.0) };
            }
        }
        let _closer = Closer(dirp);
        loop {
            // readdir signals errors only through errno.
            unsafe { *libc::__errno_location() = 0 };
            // SAFETY: dirp is a valid DIR*.
            let ent = unsafe { libc::readdir(dirp) };
            if ent.is_null() {
                let errno = unsafe { *libc::__errno_location() };
                if errno == 0 {
                    return Ok(());
                }
                return Err(io::Error::from_raw_os_error(errno)).context("readdir");
            }
            // SAFETY: d_name is NUL-terminated inside the dirent, which stays
            // valid until the next readdir; f copies what it needs.
            let name = unsafe { CStr::from_ptr((*ent).d_name.as_ptr()) };
            let bytes = name.to_bytes();
            if bytes == b"." || bytes == b".." {
                continue;
            }
            f(name)?;
        }
    }

    fn is_dir(st: &libc::stat) -> bool {
        st.st_mode & libc::S_IFMT == libc::S_IFDIR
    }

    /// Whether the walk should give this entry to `id`. Non-directories of
    /// someone else with several hard links stay as they are: one of the
    /// links may be outside the server's directory.
    fn should_chown(st: &libc::stat, id: ServerIdentity) -> bool {
        if st.st_uid == id.uid && st.st_gid == id.gid {
            return false;
        }
        is_dir(st) || st.st_nlink <= 1 || st.st_uid == id.uid
    }

    fn ignorable(e: &io::Error) -> bool {
        // The server may delete or swap entries while we walk.
        matches!(
            e.raw_os_error(),
            Some(libc::ENOENT) | Some(libc::ELOOP) | Some(libc::ENOTDIR)
        )
    }

    /// Hands every entry below `dir` to `id`. Returns how many entries were
    /// left alone (foreign hard links, too deep).
    fn repair_tree(dir: &OwnedFd, id: ServerIdentity, depth: usize) -> Result<usize> {
        let mut skipped = 0;
        for_each_entry(dir, |name| {
            let fd = match open_at(dir, name, PATH_FLAGS) {
                Ok(fd) => fd,
                Err(e) if ignorable(&e) => return Ok(()),
                Err(e) => return Err(e).context("open"),
            };
            let st = fstat(&fd).context("stat")?;
            if is_dir(&st) {
                if depth >= MAX_DEPTH {
                    skipped += 1;
                    return Ok(());
                }
                let sub = match open_at(dir, name, DIR_FLAGS) {
                    Ok(sub) => sub,
                    Err(e) if ignorable(&e) => return Ok(()),
                    Err(e) => return Err(e).context("opening a directory"),
                };
                // Decide on what was actually opened: the name may have been
                // swapped since the O_PATH open.
                let st = fstat(&sub).context("stat")?;
                if should_chown(&st, id) {
                    chown_fd(&sub, id).context("chown")?;
                }
                skipped += repair_tree(&sub, id, depth + 1)?;
            } else if should_chown(&st, id) {
                chown_fd(&fd, id).context("chown")?;
            } else if st.st_uid != id.uid {
                skipped += 1;
            }
            Ok(())
        })?;
        Ok(skipped)
    }

    fn read_high_water(path: &Path) -> Option<u32> {
        std::fs::read_to_string(path).ok()?.trim().parse().ok()
    }

    fn write_high_water(path: &Path, next: u32) -> Result<()> {
        use std::io::Write;
        use std::os::unix::fs::OpenOptionsExt;
        let tmp = path.with_extension("tmp");
        let mut f = std::fs::OpenOptions::new()
            .write(true)
            .create(true)
            .truncate(true)
            .mode(0o600)
            .open(&tmp)
            .with_context(|| format!("writing {}", tmp.display()))?;
        writeln!(f, "{next}")?;
        f.sync_all()?;
        std::fs::rename(&tmp, path).with_context(|| format!("replacing {}", path.display()))?;
        Ok(())
    }

    /// Picks the server's UID and makes its directory belong to it with mode
    /// 0700, verifying that both stuck. `Ok(None)` when the directory does
    /// not exist and `create` is false.
    pub fn claim_server_dir(
        servers_dir: &Path,
        server_id: &str,
        base: u32,
        high_water: &Path,
        create: bool,
    ) -> Result<Option<(ServerIdentity, bool)>> {
        let servers =
            open_dir(servers_dir).with_context(|| format!("opening {}", servers_dir.display()))?;
        let own_name = CString::new(server_id).context("server id")?;
        let own = match open_at(&servers, &own_name, DIR_FLAGS) {
            Ok(fd) => fd,
            Err(e) if e.raw_os_error() == Some(libc::ENOENT) && create => {
                // SAFETY: servers is a directory fd, own_name one component.
                match cvt(unsafe { libc::mkdirat(servers.as_raw_fd(), own_name.as_ptr(), 0o700) }) {
                    Ok(_) => {}
                    Err(e) if e.raw_os_error() == Some(libc::EEXIST) => {}
                    Err(e) => return Err(e).context("creating the server directory"),
                }
                open_at(&servers, &own_name, DIR_FLAGS).context("opening the server directory")?
            }
            Err(e) if e.raw_os_error() == Some(libc::ENOENT) => return Ok(None),
            Err(e) => {
                return Err(e).with_context(|| format!("opening the directory of {server_id}"))
            }
        };
        let st = fstat(&own).context("stat")?;

        let mut used = HashSet::new();
        for_each_entry(&servers, |name| {
            if name == own_name.as_c_str() {
                return Ok(());
            }
            if let Ok(other) = stat_at(&servers, name) {
                if is_dir(&other) {
                    used.insert(other.st_uid);
                }
            }
            Ok(())
        })
        .context("listing the servers directory")?;

        let hw = read_high_water(high_water);
        let (uid, fresh) =
            choose_uid(st.st_uid, &used, base, hw).context("no free server UID left")?;
        if fresh && hw.is_none_or(|h| uid >= h) {
            write_high_water(high_water, uid + 1)?;
        }
        let id = ServerIdentity { uid, gid: uid };

        let changed = st.st_uid != id.uid || st.st_gid != id.gid;
        if changed {
            chown_fd(&own, id).context("chown of the server directory")?;
        }
        if st.st_mode & 0o7777 != 0o700 {
            fchmod(&own, 0o700).context("chmod of the server directory")?;
        }
        let after = fstat(&own).context("stat")?;
        if after.st_uid != id.uid || after.st_gid != id.gid || after.st_mode & 0o7777 != 0o700 {
            bail!(
                "ownership of the server directory did not stick (owner {}:{}, mode {:o}); \
                 the file system ignores chown",
                after.st_uid,
                after.st_gid,
                after.st_mode & 0o7777
            );
        }
        Ok(Some((id, changed)))
    }

    /// The ownership walk over an existing server directory.
    pub fn repair_server_dir(
        servers_dir: &Path,
        server_id: &str,
        id: ServerIdentity,
    ) -> Result<usize> {
        let servers =
            open_dir(servers_dir).with_context(|| format!("opening {}", servers_dir.display()))?;
        let own_name = CString::new(server_id).context("server id")?;
        let own = match open_at(&servers, &own_name, DIR_FLAGS) {
            Ok(fd) => fd,
            Err(e) if e.raw_os_error() == Some(libc::ENOENT) => return Ok(0),
            Err(e) => return Err(e).context("opening the server directory"),
        };
        repair_tree(&own, id, 0)
    }

    /// Checks once that the file system under `servers_dir` keeps owners.
    pub fn probe_ownership(servers_dir: &Path, probe_uid: u32) -> Result<()> {
        std::fs::create_dir_all(servers_dir)
            .with_context(|| format!("creating {}", servers_dir.display()))?;
        let servers = open_dir(servers_dir)?;
        let name = CString::new(format!(
            ".isolation-probe-{}",
            uuid::Uuid::new_v4().simple()
        ))?;
        // SAFETY: directory fd and one component.
        cvt(unsafe { libc::mkdirat(servers.as_raw_fd(), name.as_ptr(), 0o700) })
            .context("creating a probe directory")?;
        let result = (|| -> Result<()> {
            let fd = open_at(&servers, &name, DIR_FLAGS)?;
            let probe = ServerIdentity {
                uid: probe_uid,
                gid: probe_uid,
            };
            chown_fd(&fd, probe).context("chown")?;
            let st = fstat(&fd)?;
            if st.st_uid != probe_uid || st.st_gid != probe_uid {
                bail!(
                    "chown was ignored (owner is still {}:{})",
                    st.st_uid,
                    st.st_gid
                );
            }
            Ok(())
        })();
        // SAFETY: directory fd and one component.
        unsafe { libc::unlinkat(servers.as_raw_fd(), name.as_ptr(), libc::AT_REMOVEDIR) };
        result
    }

    fn processes_of(uid: u32) -> Vec<libc::pid_t> {
        let mut pids = Vec::new();
        let Ok(entries) = std::fs::read_dir("/proc") else {
            return pids;
        };
        for e in entries.flatten() {
            let Some(pid) = e
                .file_name()
                .to_str()
                .and_then(|s| s.parse::<libc::pid_t>().ok())
            else {
                continue;
            };
            let Ok(status) = std::fs::read_to_string(e.path().join("status")) else {
                continue;
            };
            // A zombie is already dead; it only waits for its parent.
            let zombie = status
                .lines()
                .find_map(|l| l.strip_prefix("State:"))
                .is_some_and(|s| s.trim_start().starts_with('Z'));
            if zombie {
                continue;
            }
            let owned = status
                .lines()
                .find_map(|l| l.strip_prefix("Uid:"))
                .is_some_and(|ids| {
                    ids.split_whitespace()
                        .filter_map(|v| v.parse::<u32>().ok())
                        .any(|v| v == uid)
                });
            if owned {
                pids.push(pid);
            }
        }
        pids
    }

    pub fn kill_uid(id: ServerIdentity) -> Result<usize> {
        use std::os::unix::process::CommandExt;
        let found = processes_of(id.uid).len();
        for _ in 0..10 {
            let pids = processes_of(id.uid);
            if pids.is_empty() {
                return Ok(found);
            }
            let mut cmd = std::process::Command::new("/bin/true");
            cmd.uid(id.uid).gid(id.gid).env_clear();
            // SAFETY: runs after the switch to the server's UID (std sets
            // the ids before pre_exec closures) and only calls kill(2).
            unsafe {
                cmd.pre_exec(|| {
                    libc::kill(-1, libc::SIGKILL);
                    Ok(())
                });
            }
            // No root fallback that signals PIDs one by one: under fork churn
            // a PID from the list may already belong to someone else.
            if let Err(e) = cmd.status() {
                bail!("could not run the kill helper for uid {}: {e}", id.uid);
            }
            std::thread::sleep(std::time::Duration::from_millis(50));
        }
        if processes_of(id.uid).is_empty() {
            Ok(found)
        } else {
            bail!("processes of uid {} survived SIGKILL", id.uid)
        }
    }
}

#[cfg(not(target_os = "linux"))]
mod sys {
    use super::ServerIdentity;
    use anyhow::{bail, Result};
    use std::path::Path;

    pub fn claim_server_dir(
        _: &Path,
        _: &str,
        _: u32,
        _: &Path,
        _: bool,
    ) -> Result<Option<(ServerIdentity, bool)>> {
        bail!("per-server isolation is only supported on Linux")
    }
    pub fn repair_server_dir(_: &Path, _: &str, _: ServerIdentity) -> Result<usize> {
        bail!("per-server isolation is only supported on Linux")
    }
    pub fn probe_ownership(_: &Path, _: u32) -> Result<()> {
        bail!("per-server isolation is only supported on Linux")
    }
    pub fn kill_uid(_: ServerIdentity) -> Result<usize> {
        bail!("per-server isolation is only supported on Linux")
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::HashSet;

    #[test]
    fn mode_parsing() {
        assert_eq!(parse_mode("auto").unwrap(), None);
        assert_eq!(parse_mode("").unwrap(), None);
        assert_eq!(parse_mode("UID").unwrap(), Some(Mode::Uid));
        assert_eq!(parse_mode("on").unwrap(), Some(Mode::Uid));
        assert_eq!(parse_mode("off").unwrap(), Some(Mode::Off));
        assert!(parse_mode("container").is_err());
    }

    #[test]
    fn uid_base_must_stay_clear_of_users_and_nobody() {
        assert!(validate_uid_base(2_000_000).is_ok());
        assert!(validate_uid_base(1000).is_err());
        assert!(validate_uid_base(60_000).is_err()); // covers 65534
        assert!(validate_uid_base(u32::MAX - 10).is_err());
    }

    #[test]
    fn uids_are_kept_then_handed_out_upwards_and_not_reused() {
        let base = 2_000_000;
        let none = HashSet::new();
        // Root-owned dir, nothing handed out yet: the base.
        assert_eq!(choose_uid(0, &none, base, None), Some((base, true)));
        // Keeps its own UID.
        assert_eq!(
            choose_uid(base + 3, &none, base, Some(base + 9)),
            Some((base + 3, false))
        );
        // A deleted server's UID (base) is not reused: the high-water mark wins.
        let used: HashSet<u32> = [base + 1].into();
        assert_eq!(
            choose_uid(0, &used, base, Some(base + 5)),
            Some((base + 5, true))
        );
        // No high-water file (lost): above the highest UID in use.
        assert_eq!(choose_uid(0, &used, base, None), Some((base + 2, true)));
        // Shares its UID with another dir: gets a new one.
        assert_eq!(
            choose_uid(base + 1, &used, base, Some(base + 2)),
            Some((base + 2, true))
        );
        // Range exhausted: the lowest free UID is reused.
        let top = base + UID_SPAN;
        assert_eq!(choose_uid(0, &used, base, Some(top)), Some((base, true)));
    }

    #[test]
    fn account_is_appended_once_and_updated_in_place() {
        let base = "root:x:0:0:root:/root:/bin/bash\n";
        let line = "tpsrv2000000:x:2000000:2000000:TalePanel game server:/srv/a:/usr/sbin/nologin";
        let added = upsert_entry(base, "tpsrv2000000", 2_000_000, line)
            .unwrap()
            .unwrap();
        assert_eq!(added, format!("{base}{line}\n"));
        assert!(upsert_entry(&added, "tpsrv2000000", 2_000_000, line)
            .unwrap()
            .is_none());
        let moved = line.replace("/srv/a", "/srv/b");
        let updated = upsert_entry(&added, "tpsrv2000000", 2_000_000, &moved)
            .unwrap()
            .unwrap();
        assert_eq!(updated, format!("{base}{moved}\n"));
    }

    #[test]
    fn account_refuses_an_id_that_belongs_to_someone_else() {
        let content =
            "root:x:0:0:root:/root:/bin/bash\nalice:x:2000000:2000000::/home/alice:/bin/sh\n";
        assert!(upsert_entry(
            content,
            "tpsrv2000000",
            2_000_000,
            "tpsrv2000000:x:2000000:2000000::/x:/y"
        )
        .is_err());
    }

    #[test]
    fn layout_keeps_servers_enterable_and_everything_else_private() {
        let root = std::env::temp_dir().join(format!("iso-layout-{}", uuid::Uuid::new_v4()));
        std::fs::create_dir_all(root.join("servers")).unwrap();
        std::fs::create_dir_all(root.join("pki")).unwrap();
        std::fs::create_dir_all(root.join("backups")).unwrap();
        std::fs::write(root.join("machine-id"), "x").unwrap();
        let etc = std::env::temp_dir().join(format!("iso-etc-{}", uuid::Uuid::new_v4()));
        std::fs::create_dir_all(&etc).unwrap();
        let cfg = etc.join("config.toml");
        std::fs::write(&cfg, "x").unwrap();
        let mut modes = node_layout_modes(&root, &root.join("servers"), &[&cfg]);
        modes.sort();
        let mut want = vec![
            (root.clone(), 0o711),
            (root.join("servers"), 0o711),
            (root.join("pki"), 0o700),
            (root.join("backups"), 0o700),
            (root.join("machine-id"), 0o600),
            (cfg.clone(), 0o600),
        ];
        want.sort();
        assert_eq!(modes, want);
        std::fs::remove_dir_all(&root).unwrap();
        std::fs::remove_dir_all(&etc).unwrap();
    }

    #[test]
    fn disabled_isolation_does_nothing() {
        let iso = Isolation::disabled("/nonexistent/servers");
        assert!(!iso.is_enabled());
        let rt = tokio::runtime::Builder::new_current_thread()
            .build()
            .unwrap();
        assert_eq!(
            rt.block_on(iso.prepare("123e4567-e89b-12d3-a456-426614174000"))
                .unwrap(),
            None
        );
        rt.block_on(iso.repair("not-even-an-id")).unwrap();
        assert_eq!(
            rt.block_on(iso.kill_leftovers("not-even-an-id")).unwrap(),
            0
        );
    }

    /// Ownership and process tests need root. They run in the daemon's
    /// Docker test image; as a normal user they return early (pass).
    #[cfg(target_os = "linux")]
    mod as_root {
        use super::super::*;
        use std::os::unix::fs::{MetadataExt, PermissionsExt};

        const SID_A: &str = "123e4567-e89b-12d3-a456-426614174000";
        const SID_B: &str = "223e4567-e89b-12d3-a456-426614174000";
        const BASE: u32 = 3_000_000;

        fn is_root() -> bool {
            unsafe { libc::geteuid() == 0 }
        }

        fn setup() -> (PathBuf, Isolation) {
            let root = std::env::temp_dir().join(format!("iso-{}", uuid::Uuid::new_v4()));
            let servers = root.join("servers");
            std::fs::create_dir_all(&servers).unwrap();
            let etc = root.join("etc");
            std::fs::create_dir_all(&etc).unwrap();
            std::fs::write(etc.join("passwd"), "root:x:0:0:root:/root:/bin/bash\n").unwrap();
            std::fs::write(etc.join("group"), "root:x:0:\n").unwrap();
            let iso = Isolation {
                mode: Mode::Uid,
                data_root: root.clone(),
                servers_dir: servers,
                uid_base: BASE,
                etc_dir: Some(etc),
                alloc: Default::default(),
                accounts: Default::default(),
                locks: DashMap::new(),
            };
            (root, iso)
        }

        fn rt() -> tokio::runtime::Runtime {
            tokio::runtime::Builder::new_multi_thread()
                .enable_all()
                .build()
                .unwrap()
        }

        #[test]
        fn servers_get_distinct_stable_uids_and_own_their_files() {
            if !is_root() {
                return;
            }
            let (root, iso) = setup();
            let a = root.join("servers").join(SID_A);
            let b = root.join("servers").join(SID_B);
            std::fs::create_dir_all(a.join("universe/worlds")).unwrap();
            std::fs::write(a.join("universe/worlds/region.bin"), "x").unwrap();
            std::fs::create_dir_all(&b).unwrap();

            let rt = rt();
            let ida = rt.block_on(iso.prepare(SID_A)).unwrap().unwrap();
            let idb = rt.block_on(iso.prepare(SID_B)).unwrap().unwrap();
            assert_ne!(ida.uid, idb.uid);
            assert!(ida.uid >= BASE && idb.uid >= BASE);
            assert_eq!(rt.block_on(iso.prepare(SID_A)).unwrap().unwrap(), ida);

            let md = std::fs::metadata(&a).unwrap();
            assert_eq!((md.uid(), md.gid()), (ida.uid, ida.gid));
            assert_eq!(md.permissions().mode() & 0o7777, 0o700);
            let f = std::fs::metadata(a.join("universe/worlds/region.bin")).unwrap();
            assert_eq!(f.uid(), ida.uid);

            std::fs::write(a.join("config.json"), "{}").unwrap();
            rt.block_on(iso.repair(SID_A)).unwrap();
            assert_eq!(
                std::fs::metadata(a.join("config.json")).unwrap().uid(),
                ida.uid
            );

            let passwd = std::fs::read_to_string(root.join("etc/passwd")).unwrap();
            assert!(passwd.contains(&format!("tpsrv{}:x:{}:{}:", ida.uid, ida.uid, ida.gid)));
            assert!(passwd.contains(&a.display().to_string()));
            std::fs::remove_dir_all(&root).unwrap();
        }

        #[test]
        fn a_deleted_servers_uid_is_not_handed_to_the_next_server() {
            if !is_root() {
                return;
            }
            let (root, iso) = setup();
            let a = root.join("servers").join(SID_A);
            std::fs::create_dir_all(&a).unwrap();
            let rt = rt();
            let ida = rt.block_on(iso.prepare(SID_A)).unwrap().unwrap();
            std::fs::remove_dir_all(&a).unwrap();
            let b = root.join("servers").join(SID_B);
            std::fs::create_dir_all(&b).unwrap();
            let idb = rt.block_on(iso.prepare(SID_B)).unwrap().unwrap();
            assert!(idb.uid > ida.uid, "UID {} was reused", ida.uid);
            std::fs::remove_dir_all(&root).unwrap();
        }

        #[test]
        fn identity_for_creates_a_missing_dir_owned_by_the_server() {
            if !is_root() {
                return;
            }
            let (root, iso) = setup();
            let id = rt().block_on(iso.identity_for(SID_A)).unwrap().unwrap();
            let md = std::fs::metadata(root.join("servers").join(SID_A)).unwrap();
            assert_eq!(md.uid(), id.uid);
            assert_eq!(md.permissions().mode() & 0o7777, 0o700);
            std::fs::remove_dir_all(&root).unwrap();
        }

        #[test]
        fn identity_for_hands_over_the_contents_of_a_dir_it_takes_over() {
            if !is_root() {
                return;
            }
            let (root, iso) = setup();
            let a = root.join("servers").join(SID_A);
            std::fs::create_dir_all(a.join("mods")).unwrap();
            std::fs::write(a.join("mods/x.jar"), "x").unwrap();
            let id = rt().block_on(iso.identity_for(SID_A)).unwrap().unwrap();
            assert_eq!(std::fs::metadata(a.join("mods")).unwrap().uid(), id.uid);
            assert_eq!(
                std::fs::metadata(a.join("mods/x.jar")).unwrap().uid(),
                id.uid
            );
            std::fs::remove_dir_all(&root).unwrap();
        }

        #[test]
        fn walk_never_follows_symlinks_out_of_the_server_dir() {
            if !is_root() {
                return;
            }
            let (root, iso) = setup();
            let a = root.join("servers").join(SID_A);
            std::fs::create_dir_all(&a).unwrap();
            let secret_dir = root.join("pki");
            std::fs::create_dir_all(&secret_dir).unwrap();
            std::fs::write(secret_dir.join("node-key.pem"), "secret").unwrap();
            std::os::unix::fs::symlink(&secret_dir, a.join("mods")).unwrap();
            std::os::unix::fs::symlink(secret_dir.join("node-key.pem"), a.join("key.pem")).unwrap();

            let id = rt().block_on(iso.prepare(SID_A)).unwrap().unwrap();
            assert_eq!(
                std::fs::metadata(secret_dir.join("node-key.pem"))
                    .unwrap()
                    .uid(),
                0
            );
            assert_eq!(std::fs::metadata(&secret_dir).unwrap().uid(), 0);
            assert_eq!(
                std::fs::symlink_metadata(a.join("mods")).unwrap().uid(),
                id.uid
            );
            assert_eq!(
                std::fs::symlink_metadata(a.join("key.pem")).unwrap().uid(),
                id.uid
            );
            std::fs::remove_dir_all(&root).unwrap();
        }

        #[test]
        fn hard_links_to_foreign_files_are_left_alone() {
            if !is_root() {
                return;
            }
            let (root, iso) = setup();
            let a = root.join("servers").join(SID_A);
            std::fs::create_dir_all(&a).unwrap();
            let outside = root.join("outside.txt");
            std::fs::write(&outside, "x").unwrap();
            std::fs::hard_link(&outside, a.join("link.txt")).unwrap();
            rt().block_on(iso.prepare(SID_A)).unwrap();
            assert_eq!(std::fs::metadata(&outside).unwrap().uid(), 0);
            std::fs::remove_dir_all(&root).unwrap();
        }

        #[test]
        fn a_uid_shared_by_two_dirs_is_reassigned() {
            if !is_root() {
                return;
            }
            let (root, iso) = setup();
            let a = root.join("servers").join(SID_A);
            let b = root.join("servers").join(SID_B);
            std::fs::create_dir_all(&a).unwrap();
            std::fs::create_dir_all(&b).unwrap();
            std::os::unix::fs::chown(&a, Some(BASE), Some(BASE)).unwrap();
            std::os::unix::fs::chown(&b, Some(BASE), Some(BASE)).unwrap();
            let rt = rt();
            let idb = rt.block_on(iso.prepare(SID_B)).unwrap().unwrap();
            assert_ne!(idb.uid, BASE);
            assert_eq!(rt.block_on(iso.prepare(SID_A)).unwrap().unwrap().uid, BASE);
            std::fs::remove_dir_all(&root).unwrap();
        }

        #[test]
        fn claim_all_closes_never_started_servers() {
            if !is_root() {
                return;
            }
            let (root, iso) = setup();
            let a = root.join("servers").join(SID_A);
            std::fs::create_dir_all(&a).unwrap();
            std::fs::set_permissions(&a, std::fs::Permissions::from_mode(0o755)).unwrap();
            std::fs::create_dir_all(root.join("servers/not-a-server")).unwrap();
            rt().block_on(iso.claim_all());
            let md = std::fs::metadata(&a).unwrap();
            assert!(md.uid() >= BASE);
            assert_eq!(md.permissions().mode() & 0o7777, 0o700);
            assert_eq!(
                std::fs::metadata(root.join("servers/not-a-server"))
                    .unwrap()
                    .uid(),
                0
            );
            std::fs::remove_dir_all(&root).unwrap();
        }

        #[test]
        fn leftover_processes_of_a_server_are_killed() {
            if !is_root() {
                return;
            }
            let (root, iso) = setup();
            let a = root.join("servers").join(SID_A);
            std::fs::create_dir_all(&a).unwrap();
            let rt = rt();
            let id = rt.block_on(iso.prepare(SID_A)).unwrap().unwrap();
            use std::os::unix::process::CommandExt;
            let mut child = std::process::Command::new("sleep")
                .arg("60")
                .uid(id.uid)
                .gid(id.gid)
                .spawn()
                .unwrap();
            assert_eq!(rt.block_on(iso.kill_leftovers(SID_A)).unwrap(), 1);
            assert!(!child.wait().unwrap().success());
            std::fs::remove_dir_all(&root).unwrap();
        }

        #[test]
        fn missing_server_dir_is_not_an_error() {
            if !is_root() {
                return;
            }
            let (root, iso) = setup();
            assert_eq!(rt().block_on(iso.prepare(SID_A)).unwrap(), None);
            std::fs::remove_dir_all(&root).unwrap();
        }
    }
}
