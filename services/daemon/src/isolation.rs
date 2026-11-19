//! Per-server process isolation (roadmap "Now" #9, step 1).
//!
//! Without isolation every game server runs as the daemon's user (root in the
//! container) and can read the node token (`/proc/1/environ`), the node's TLS
//! key, cloud backup credentials and every other server's files and game
//! session. With isolation:
//!
//! * every server gets its own unprivileged UID/GID, `server_uid_base + n`.
//!   The UID is recorded nowhere but in the owner of the server's directory,
//!   so it survives daemon restarts and needs no state file;
//! * the server's directory (`{data_root}/servers/<id>`) belongs to that UID
//!   with mode 0700, so servers cannot see each other;
//! * the JVM is spawned with that UID/GID, no supplementary groups,
//!   `no_new_privs` and umask 007 (see `process::hytale`);
//! * the node's own files (`data_root` siblings of `servers/`, the PKI dir,
//!   the config file) are made root-only at startup;
//! * files the daemon writes into a server's directory (uploads, mods,
//!   restores, provisioning) are handed to the server's UID afterwards
//!   ([`Isolation::repair`]), so the server can change them while it runs.
//!
//! The ownership walk runs as root inside a directory the (untrusted) server
//! controls. It therefore works on directory file descriptors opened with
//! `O_NOFOLLOW`, changes owners with `AT_SYMLINK_NOFOLLOW` and never follows a
//! symlink, so swapping a directory for a symlink mid-walk cannot redirect it
//! to files outside the server's directory. Regular files owned by someone
//! else that have more than one hard link are left alone, so a hard link to a
//! foreign file cannot be used to take it over.

use std::path::{Path, PathBuf};
use std::sync::Arc;

use anyhow::{bail, Context, Result};
use tracing::{info, warn};

use crate::config::Config;

/// Number of UIDs reserved for game servers, starting at `server_uid_base`.
pub const UID_SPAN: u32 = 65_536;

/// Directory nesting the ownership walk follows before it gives up.
const MAX_DEPTH: usize = 128;

/// The unprivileged identity a game server runs under.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
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

/// Shared isolation state. Cheap to clone via `Arc`.
pub struct Isolation {
    mode: Mode,
    servers_dir: PathBuf,
    uid_base: u32,
    /// Where `passwd` and `group` live (`/etc`; a temp dir in tests). `None`
    /// skips account entries.
    etc_dir: Option<PathBuf>,
    /// Serialises UID allocation so two servers never get the same UID.
    alloc: Arc<std::sync::Mutex<()>>,
}

impl Isolation {
    /// Isolation switched off, for tests.
    #[cfg(test)]
    pub fn disabled(servers_dir: impl Into<PathBuf>) -> Self {
        Self {
            mode: Mode::Off,
            servers_dir: servers_dir.into(),
            uid_base: crate::config::DaemonConfig::default_server_uid_base(),
            etc_dir: None,
            alloc: Arc::new(std::sync::Mutex::new(())),
        }
    }

    /// Resolves the configured mode. `auto` turns isolation on when the
    /// daemon is root on Linux. `uid` fails when that is not the case, so an
    /// operator who asked for isolation never silently runs without it.
    pub fn from_config(config: &Config) -> Result<Self> {
        let servers_dir = PathBuf::from(format!(
            "{}/servers",
            config.daemon.data_root.trim_end_matches('/')
        ));
        let requested = parse_mode(&config.daemon.server_isolation)?;
        let supported = platform_supported();
        let mode = match requested {
            Some(Mode::Off) => Mode::Off,
            Some(Mode::Uid) if !supported => bail!(
                "server_isolation=uid needs the daemon to run as root on Linux; \
                 run it as root or set TALEDAEMON_SERVER_ISOLATION=off"
            ),
            Some(Mode::Uid) => Mode::Uid,
            None if supported => Mode::Uid,
            None => Mode::Off,
        };
        if mode == Mode::Uid {
            validate_uid_base(config.daemon.server_uid_base)?;
        }
        Ok(Self {
            mode,
            servers_dir,
            uid_base: config.daemon.server_uid_base,
            etc_dir: Some(PathBuf::from("/etc")),
            alloc: Arc::new(std::sync::Mutex::new(())),
        })
    }

    pub fn is_enabled(&self) -> bool {
        self.mode == Mode::Uid
    }

    /// Prepares a server for a start: picks its UID, makes sure the account
    /// exists, hands its directory to it. `Ok(None)` when isolation is off.
    /// An error means the server must not start.
    pub async fn prepare(&self, server_id: &str) -> Result<Option<ServerIdentity>> {
        if !self.is_enabled() {
            return Ok(None);
        }
        let id = self.claim(server_id).await?;
        if let (Some(id), Some(etc)) = (id, self.etc_dir.clone()) {
            let home = self.servers_dir.join(server_id);
            let result = tokio::task::spawn_blocking(move || ensure_account(&etc, id, &home))
                .await
                .context("account task panicked")?;
            // Without an entry the JVM still runs; it only lacks a user name
            // (user.name "?", shared hsperfdata). Not worth refusing a start.
            if let Err(e) = result {
                warn!(server_id, uid = id.uid, error = %e, "Could not add the server's account to /etc/passwd");
            }
        }
        Ok(id)
    }

    /// Hands everything in the server's directory to its UID. Call after the
    /// daemon wrote into it. No-op when isolation is off or the directory
    /// does not exist.
    pub async fn repair(&self, server_id: &str) -> Result<()> {
        if !self.is_enabled() {
            return Ok(());
        }
        self.claim(server_id).await.map(|_| ())
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
                warn!(servers_dir = %self.servers_dir.display(), error = %e, "Could not list server directories");
                return;
            }
        };
        let mut claimed = 0usize;
        for id in &ids {
            match self.claim(id).await {
                Ok(_) => claimed += 1,
                Err(e) => {
                    warn!(server_id = %id, error = %e, "Could not hand the server directory to its user")
                }
            }
        }
        info!(
            claimed,
            total = ids.len(),
            "Server directories handed to their own users"
        );
    }

    async fn claim(&self, server_id: &str) -> Result<Option<ServerIdentity>> {
        if !is_canonical_uuid(server_id) {
            bail!("refusing to isolate {server_id:?}: not a server id");
        }
        let servers_dir = self.servers_dir.clone();
        let server_id = server_id.to_string();
        let base = self.uid_base;
        let alloc = Arc::clone(&self.alloc);
        tokio::task::spawn_blocking(move || {
            let _guard = alloc.lock().unwrap_or_else(|e| e.into_inner());
            sys::claim_server_dir(&servers_dir, &server_id, base)
        })
        .await
        .context("isolation task panicked")?
    }

    /// Makes the node's own files root-only. Called once at startup when
    /// isolation is on. Failures are logged, not fatal: a node that cannot
    /// tighten a permission is no worse off than before.
    pub fn secure_node_layout(&self, data_root: &Path, extra_private: &[&Path]) {
        if !self.is_enabled() {
            return;
        }
        for (path, mode) in node_layout_modes(data_root, &self.servers_dir, extra_private) {
            if let Err(e) = set_mode_nofollow(&path, mode) {
                warn!(path = %path.display(), error = %e, "Could not tighten permissions");
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
// /etc/passwd and /etc/group
// ─────────────────────────────────────────────────────────────────────────────

/// Adds (or updates) `tpsrv<uid>` in `{etc}/passwd` and `{etc}/group`, so the
/// JVM sees a user name and a home directory.
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
    let current =
        std::fs::read_to_string(path).with_context(|| format!("reading {}", path.display()))?;
    let Some(updated) = upsert_entry(&current, name, id, line)? else {
        return Ok(());
    };
    let tmp = path.with_file_name(format!(
        ".{}.taledaemon",
        path.file_name().and_then(|n| n.to_str()).unwrap_or("db")
    ));
    std::fs::write(&tmp, updated).with_context(|| format!("writing {}", tmp.display()))?;
    {
        use std::os::unix::fs::PermissionsExt;
        std::fs::set_permissions(&tmp, std::fs::Permissions::from_mode(0o644))?;
    }
    std::fs::rename(&tmp, path).with_context(|| format!("replacing {}", path.display()))?;
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
    use super::{ServerIdentity, MAX_DEPTH, UID_SPAN};
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

    pub fn open_dir(path: &Path) -> io::Result<OwnedFd> {
        let c = CString::new(path.as_os_str().as_bytes())
            .map_err(|_| io::Error::from(io::ErrorKind::InvalidInput))?;
        // SAFETY: valid NUL-terminated path; the fd is owned by the result.
        let fd = cvt(unsafe { libc::open(c.as_ptr(), DIR_FLAGS) })?;
        Ok(unsafe { OwnedFd::from_raw_fd(fd) })
    }

    pub fn open_dir_at(dir: &OwnedFd, name: &CStr) -> io::Result<OwnedFd> {
        // SAFETY: dir is an open directory fd, name a single component.
        let fd = cvt(unsafe { libc::openat(dir.as_raw_fd(), name.as_ptr(), DIR_FLAGS) })?;
        Ok(unsafe { OwnedFd::from_raw_fd(fd) })
    }

    pub fn stat_at(dir: &OwnedFd, name: &CStr) -> io::Result<libc::stat> {
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

    pub fn fstat(fd: &OwnedFd) -> io::Result<libc::stat> {
        let mut st: libc::stat = unsafe { std::mem::zeroed() };
        // SAFETY: valid fd and out-pointer.
        cvt(unsafe { libc::fstat(fd.as_raw_fd(), &mut st) })?;
        Ok(st)
    }

    fn chown_at(dir: &OwnedFd, name: &CStr, id: ServerIdentity) -> io::Result<()> {
        // SAFETY: AT_SYMLINK_NOFOLLOW changes a symlink itself, never its target.
        cvt(unsafe {
            libc::fchownat(
                dir.as_raw_fd(),
                name.as_ptr(),
                id.uid,
                id.gid,
                libc::AT_SYMLINK_NOFOLLOW,
            )
        })
        .map(|_| ())
    }

    fn fchown(fd: &OwnedFd, id: ServerIdentity) -> io::Result<()> {
        // SAFETY: valid fd.
        cvt(unsafe { libc::fchown(fd.as_raw_fd(), id.uid, id.gid) }).map(|_| ())
    }

    fn fchmod(fd: &OwnedFd, mode: libc::mode_t) -> io::Result<()> {
        // SAFETY: valid fd.
        cvt(unsafe { libc::fchmod(fd.as_raw_fd(), mode) }).map(|_| ())
    }

    /// Names in a directory, without `.` and `..`.
    pub fn list(dir: &OwnedFd) -> io::Result<Vec<CString>> {
        // fdopendir takes ownership of the fd it gets; hand it a duplicate.
        // SAFETY: dup of a valid fd.
        let dup = cvt(unsafe { libc::fcntl(dir.as_raw_fd(), libc::F_DUPFD_CLOEXEC, 0) })?;
        // SAFETY: dup is a valid directory fd; on success the DIR owns it.
        let dirp = unsafe { libc::fdopendir(dup) };
        if dirp.is_null() {
            let e = io::Error::last_os_error();
            unsafe { libc::close(dup) };
            return Err(e);
        }
        let mut names = Vec::new();
        let result = loop {
            // readdir signals errors only through errno.
            unsafe { *libc::__errno_location() = 0 };
            // SAFETY: dirp is a valid DIR*.
            let ent = unsafe { libc::readdir(dirp) };
            if ent.is_null() {
                let errno = unsafe { *libc::__errno_location() };
                break if errno == 0 {
                    Ok(())
                } else {
                    Err(io::Error::from_raw_os_error(errno))
                };
            }
            // SAFETY: d_name is NUL-terminated inside the dirent.
            let name = unsafe { CStr::from_ptr((*ent).d_name.as_ptr()) };
            let bytes = name.to_bytes();
            if bytes == b"." || bytes == b".." {
                continue;
            }
            names.push(name.to_owned());
        };
        // SAFETY: dirp is valid and closed exactly once (also closes dup).
        unsafe { libc::closedir(dirp) };
        result.map(|_| names)
    }

    fn is_dir(st: &libc::stat) -> bool {
        st.st_mode & libc::S_IFMT == libc::S_IFDIR
    }

    /// Whether the walk should give this entry to `id`. Regular files of
    /// someone else with several hard links stay as they are: one of the
    /// links may be outside the server's directory.
    pub fn should_chown(st: &libc::stat, id: ServerIdentity) -> bool {
        if st.st_uid == id.uid && st.st_gid == id.gid {
            return false;
        }
        let regular = st.st_mode & libc::S_IFMT == libc::S_IFREG;
        !(regular && st.st_nlink > 1 && st.st_uid != id.uid)
    }

    fn ignorable(e: &io::Error) -> bool {
        // The server may delete or swap entries while we walk.
        matches!(
            e.raw_os_error(),
            Some(libc::ENOENT) | Some(libc::ELOOP) | Some(libc::ENOTDIR)
        )
    }

    /// Hands every entry below `dir` to `id`. Returns how many hard-linked
    /// foreign files were skipped.
    pub fn repair_tree(dir: &OwnedFd, id: ServerIdentity, depth: usize) -> Result<usize> {
        if depth > MAX_DEPTH {
            bail!("directory tree deeper than {MAX_DEPTH} levels");
        }
        let mut skipped = 0;
        for name in list(dir).context("listing a directory")? {
            let st = match stat_at(dir, &name) {
                Ok(st) => st,
                Err(e) if ignorable(&e) => continue,
                Err(e) => return Err(e).context("stat"),
            };
            if is_dir(&st) {
                let sub = match open_dir_at(dir, &name) {
                    Ok(fd) => fd,
                    Err(e) if ignorable(&e) => continue,
                    Err(e) => return Err(e).context("opening a directory"),
                };
                // Decide on what was actually opened, not on the earlier stat.
                let st = fstat(&sub).context("stat")?;
                if should_chown(&st, id) {
                    fchown(&sub, id).context("chown")?;
                }
                skipped += repair_tree(&sub, id, depth + 1)?;
            } else if should_chown(&st, id) {
                match chown_at(dir, &name, id) {
                    Ok(()) => {}
                    Err(e) if ignorable(&e) => {}
                    Err(e) => return Err(e).context("chown"),
                }
            } else if st.st_uid != id.uid {
                skipped += 1;
            }
        }
        Ok(skipped)
    }

    /// Picks the server's UID and hands its directory to it. `Ok(None)` when
    /// the directory does not exist (yet).
    pub fn claim_server_dir(
        servers_dir: &Path,
        server_id: &str,
        base: u32,
    ) -> Result<Option<ServerIdentity>> {
        let servers =
            open_dir(servers_dir).with_context(|| format!("opening {}", servers_dir.display()))?;
        let own_name = CString::new(server_id).context("server id")?;
        let own = match open_dir_at(&servers, &own_name) {
            Ok(fd) => fd,
            Err(e) if e.raw_os_error() == Some(libc::ENOENT) => return Ok(None),
            Err(e) => {
                return Err(e).with_context(|| format!("opening the directory of {server_id}"))
            }
        };
        let st = fstat(&own).context("stat")?;

        let in_range = |uid: u32| uid >= base && uid - base < UID_SPAN;
        let mut used = HashSet::new();
        for name in list(&servers).context("listing the servers directory")? {
            if name.as_c_str() == own_name.as_c_str() {
                continue;
            }
            if let Ok(other) = stat_at(&servers, &name) {
                if is_dir(&other) && in_range(other.st_uid) {
                    used.insert(other.st_uid);
                }
            }
        }
        let uid = if in_range(st.st_uid) && !used.contains(&st.st_uid) {
            st.st_uid
        } else {
            (base..base + UID_SPAN)
                .find(|u| !used.contains(u))
                .context("no free server UID left")?
        };
        let id = ServerIdentity { uid, gid: uid };

        if st.st_uid != id.uid || st.st_gid != id.gid {
            fchown(&own, id).context("chown of the server directory")?;
        }
        if st.st_mode & 0o7777 != 0o700 {
            fchmod(&own, 0o700).context("chmod of the server directory")?;
        }
        let skipped = repair_tree(&own, id, 0)?;
        if skipped > 0 {
            tracing::warn!(
                server_id,
                skipped,
                "Left hard-linked files of other owners in the server directory alone"
            );
        }
        Ok(Some(id))
    }
}

#[cfg(not(target_os = "linux"))]
mod sys {
    use super::ServerIdentity;
    use anyhow::{bail, Result};
    use std::path::Path;

    pub fn claim_server_dir(_: &Path, _: &str, _: u32) -> Result<Option<ServerIdentity>> {
        bail!("per-server isolation is only supported on Linux")
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;

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
        assert!(validate_uid_base(200_000).is_ok());
        assert!(validate_uid_base(1000).is_err());
        assert!(validate_uid_base(60_000).is_err()); // covers 65534
        assert!(validate_uid_base(u32::MAX - 10).is_err());
    }

    #[test]
    fn account_is_appended_once_and_updated_in_place() {
        let base = "root:x:0:0:root:/root:/bin/bash\n";
        let line = "tpsrv200000:x:200000:200000:TalePanel game server:/srv/a:/usr/sbin/nologin";
        let added = upsert_entry(base, "tpsrv200000", 200_000, line)
            .unwrap()
            .unwrap();
        assert_eq!(added, format!("{base}{line}\n"));
        // Already right: nothing to write.
        assert!(upsert_entry(&added, "tpsrv200000", 200_000, line)
            .unwrap()
            .is_none());
        // Home changed (UID now belongs to another server): replaced, not duplicated.
        let moved = line.replace("/srv/a", "/srv/b");
        let updated = upsert_entry(&added, "tpsrv200000", 200_000, &moved)
            .unwrap()
            .unwrap();
        assert_eq!(updated, format!("{base}{moved}\n"));
    }

    #[test]
    fn account_refuses_an_id_that_belongs_to_someone_else() {
        let content =
            "root:x:0:0:root:/root:/bin/bash\nalice:x:200000:200000::/home/alice:/bin/sh\n";
        assert!(upsert_entry(
            content,
            "tpsrv200000",
            200_000,
            "tpsrv200000:x:200000:200000::/x:/y"
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
    }

    /// The fd-based walk needs root to chown. Runs in the daemon's Docker
    /// build/test image; skipped (passes) as a normal user.
    #[cfg(target_os = "linux")]
    mod as_root {
        use super::super::*;
        use std::os::unix::fs::{MetadataExt, PermissionsExt};

        const SID_A: &str = "123e4567-e89b-12d3-a456-426614174000";
        const SID_B: &str = "223e4567-e89b-12d3-a456-426614174000";

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
                servers_dir: servers,
                uid_base: 300_000,
                etc_dir: Some(etc),
                alloc: Arc::new(std::sync::Mutex::new(())),
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
            assert!(ida.uid >= 300_000 && idb.uid >= 300_000);
            // Stable across calls.
            assert_eq!(rt.block_on(iso.prepare(SID_A)).unwrap().unwrap(), ida);

            let md = std::fs::metadata(&a).unwrap();
            assert_eq!((md.uid(), md.gid()), (ida.uid, ida.gid));
            assert_eq!(md.permissions().mode() & 0o7777, 0o700);
            let f = std::fs::metadata(a.join("universe/worlds/region.bin")).unwrap();
            assert_eq!(f.uid(), ida.uid);

            // A file the daemon (root) writes later is handed over by repair.
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
            // The links themselves now belong to the server; their targets don't.
            assert_eq!(
                std::fs::symlink_metadata(a.join("mods")).unwrap().uid(),
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
            std::os::unix::fs::chown(&a, Some(300_000), Some(300_000)).unwrap();
            std::os::unix::fs::chown(&b, Some(300_000), Some(300_000)).unwrap();
            let rt = rt();
            let idb = rt.block_on(iso.prepare(SID_B)).unwrap().unwrap();
            assert_ne!(idb.uid, 300_000);
            assert_eq!(
                rt.block_on(iso.prepare(SID_A)).unwrap().unwrap().uid,
                300_000
            );
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
            assert!(md.uid() >= 300_000);
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
