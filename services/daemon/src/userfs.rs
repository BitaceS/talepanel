//! File work inside a server's directory with that server's file permissions.
//!
//! With per-server isolation the game server is an unprivileged process that
//! owns its directory and can swap any entry in it at any moment, for example
//! a directory for a symlink to `/srv/taledaemon/pki`. Path checks done by a
//! root daemon cannot win that race. So the daemon does file work in a server
//! directory with the server's own *file system* identity (Linux `fsuid` /
//! `fsgid`, which are per thread). Switching away from fsuid 0 also drops
//! CAP_DAC_OVERRIDE and the other file capabilities for that thread, so the
//! kernel itself refuses any path that leads to a file the server could not
//! open, however the path was raced.
//!
//! Two ways in:
//! * [`FsUserGuard`] switches the current thread for a block of *blocking*
//!   code (inside `spawn_blocking`). It is `!Send`, so it cannot be held
//!   across an `.await` in a future that may move between threads.
//! * [`run_async`] runs a whole future on a small runtime whose every thread
//!   (workers and blocking pool) carries the server's fsuid, so `tokio::fs`
//!   inside it is covered too.
//!
//! Do not spawn processes from inside either: a child would inherit the
//! server's fsuid.
//!
//! With `None` (isolation off) both run the work unchanged as the daemon.

use std::future::Future;
use std::io;
use std::marker::PhantomData;

use crate::isolation::ServerIdentity;

/// While alive, file system calls on this thread use the server's fsuid/fsgid.
/// Dropping it restores the previous ids.
pub struct FsUserGuard {
    previous: Option<(u32, u32)>,
    _not_send: PhantomData<*const ()>,
}

impl FsUserGuard {
    pub fn enter(id: Option<ServerIdentity>) -> io::Result<Self> {
        let previous = match id {
            Some(id) => Some(sys::switch(id.uid, id.gid)?),
            None => None,
        };
        Ok(Self {
            previous,
            _not_send: PhantomData,
        })
    }
}

impl Drop for FsUserGuard {
    fn drop(&mut self) {
        if let Some((uid, gid)) = self.previous {
            // A thread that cannot get its identity back would run later,
            // unrelated work with the wrong permissions. That must not happen.
            if sys::switch(uid, gid).is_err() {
                tracing::error!("could not restore the daemon's file system identity; aborting");
                std::process::abort();
            }
        }
    }
}

/// The identity to read a server directory with, from its metadata: its
/// owner, when the daemon is root and the owner is not. `None` otherwise
/// (isolation off, or a directory not yet handed to a server).
pub fn owner_identity(md: &std::fs::Metadata) -> Option<ServerIdentity> {
    use std::os::unix::fs::MetadataExt;
    #[cfg(target_os = "linux")]
    // SAFETY: geteuid has no preconditions.
    let daemon_is_root = unsafe { libc::geteuid() == 0 };
    #[cfg(not(target_os = "linux"))]
    let daemon_is_root = false;
    (daemon_is_root && md.uid() != 0).then(|| ServerIdentity {
        uid: md.uid(),
        gid: md.gid(),
    })
}

/// Runs blocking `f` with the server's file permissions.
pub async fn run_blocking<F, T>(id: Option<ServerIdentity>, f: F) -> io::Result<T>
where
    F: FnOnce() -> T + Send + 'static,
    T: Send + 'static,
{
    tokio::task::spawn_blocking(move || {
        let _guard = FsUserGuard::enter(id)?;
        Ok(f())
    })
    .await
    .map_err(join_error)?
}

/// Runs `fut` with the server's file permissions, on a runtime reserved for
/// that identity.
pub async fn run_async<F, T>(id: Option<ServerIdentity>, fut: F) -> io::Result<T>
where
    F: Future<Output = T> + Send + 'static,
    T: Send + 'static,
{
    let Some(id) = id else {
        return Ok(fut.await);
    };
    let handle = sys::runtime_for(id)?;
    handle
        .spawn(async move {
            // Belt and braces: never run tenant file work on a thread that
            // is not switched.
            if sys::current() != Some((id.uid, id.gid)) {
                return Err(io::Error::new(
                    io::ErrorKind::PermissionDenied,
                    "worker thread does not carry the server's file identity",
                ));
            }
            Ok(fut.await)
        })
        .await
        .map_err(join_error)?
}

fn join_error(e: tokio::task::JoinError) -> io::Error {
    if e.is_panic() {
        std::panic::resume_unwind(e.into_panic());
    }
    io::Error::other(e)
}

#[cfg(target_os = "linux")]
mod sys {
    use super::ServerIdentity;
    use std::collections::HashMap;
    use std::io;
    use std::sync::{Mutex, OnceLock};

    /// Current fsuid/fsgid of this thread. setfsuid(-1) changes nothing and
    /// returns the current value.
    pub fn current() -> Option<(u32, u32)> {
        // SAFETY: plain syscalls without pointers.
        unsafe {
            let uid = libc::setfsuid(u32::MAX as libc::uid_t) as u32;
            let gid = libc::setfsgid(u32::MAX as libc::gid_t) as u32;
            Some((uid, gid))
        }
    }

    /// Sets fsgid then fsuid and returns the previous pair. setfsuid reports
    /// no errors, so the result is read back.
    pub fn switch(uid: u32, gid: u32) -> io::Result<(u32, u32)> {
        // SAFETY: plain syscalls without pointers; they affect this thread only.
        let prev_gid = unsafe { libc::setfsgid(gid as libc::gid_t) } as u32;
        let prev_uid = unsafe { libc::setfsuid(uid as libc::uid_t) } as u32;
        if current() != Some((uid, gid)) {
            return Err(io::Error::new(
                io::ErrorKind::PermissionDenied,
                format!("could not switch the file system identity to {uid}:{gid}"),
            ));
        }
        Ok((prev_uid, prev_gid))
    }

    type Runtimes = Mutex<HashMap<(u32, u32), &'static tokio::runtime::Runtime>>;

    /// One small runtime per identity, built on first use and kept for the
    /// life of the daemon (a runtime must not be dropped in async context).
    pub fn runtime_for(id: ServerIdentity) -> io::Result<tokio::runtime::Handle> {
        static RUNTIMES: OnceLock<Runtimes> = OnceLock::new();
        let map = RUNTIMES.get_or_init(Default::default);
        let mut map = map.lock().unwrap_or_else(|e| e.into_inner());
        if let Some(rt) = map.get(&(id.uid, id.gid)) {
            return Ok(rt.handle().clone());
        }
        let (uid, gid) = (id.uid, id.gid);
        let rt = tokio::runtime::Builder::new_multi_thread()
            .worker_threads(1)
            .max_blocking_threads(16)
            .thread_name(format!("srvfs-{uid}"))
            .enable_all()
            .on_thread_start(move || {
                if switch(uid, gid).is_err() {
                    tracing::error!(uid, "could not switch a file worker thread; aborting");
                    std::process::abort();
                }
            })
            .build()?;
        let rt: &'static tokio::runtime::Runtime = Box::leak(Box::new(rt));
        map.insert((uid, gid), rt);
        Ok(rt.handle().clone())
    }
}

#[cfg(not(target_os = "linux"))]
mod sys {
    use super::ServerIdentity;
    use std::io;

    pub fn current() -> Option<(u32, u32)> {
        None
    }

    pub fn switch(_: u32, _: u32) -> io::Result<(u32, u32)> {
        Err(io::Error::new(
            io::ErrorKind::Unsupported,
            "per-server file identities need Linux",
        ))
    }

    pub fn runtime_for(_: ServerIdentity) -> io::Result<tokio::runtime::Handle> {
        Err(io::Error::new(
            io::ErrorKind::Unsupported,
            "per-server file identities need Linux",
        ))
    }
}

#[cfg(all(test, target_os = "linux"))]
mod tests {
    use super::*;
    use std::os::unix::fs::{MetadataExt, PermissionsExt};

    fn is_root() -> bool {
        unsafe { libc::geteuid() == 0 }
    }

    fn scratch() -> std::path::PathBuf {
        let d = std::env::temp_dir().join(format!("userfs-{}", uuid::Uuid::new_v4()));
        std::fs::create_dir_all(&d).unwrap();
        d
    }

    const ID: ServerIdentity = ServerIdentity {
        uid: 2_100_000,
        gid: 2_100_000,
    };

    #[test]
    fn none_runs_as_the_daemon() {
        let rt = tokio::runtime::Runtime::new().unwrap();
        assert_eq!(rt.block_on(run_blocking(None, || 7)).unwrap(), 7);
        assert_eq!(rt.block_on(run_async(None, async { 8 })).unwrap(), 8);
    }

    /// The kernel, not path checks, keeps the server out of root's files:
    /// a symlink that leads to a root-only file cannot be followed.
    #[test]
    fn guard_blocks_root_only_files_even_through_symlinks() {
        if !is_root() {
            return;
        }
        let d = scratch();
        let secret = d.join("secret");
        std::fs::create_dir(&secret).unwrap();
        std::fs::set_permissions(&secret, std::fs::Permissions::from_mode(0o700)).unwrap();
        std::fs::write(secret.join("key.pem"), "s").unwrap();
        let own = d.join("own");
        std::fs::create_dir(&own).unwrap();
        std::os::unix::fs::chown(&own, Some(ID.uid), Some(ID.gid)).unwrap();
        std::fs::set_permissions(&d, std::fs::Permissions::from_mode(0o711)).unwrap();
        std::os::unix::fs::symlink(&secret, own.join("link")).unwrap();

        let (own2, secret2) = (own.clone(), secret.clone());
        let rt = tokio::runtime::Runtime::new().unwrap();
        let (through_link, direct, written_uid) = rt
            .block_on(run_blocking(Some(ID), move || {
                let through = std::fs::read(own2.join("link/key.pem")).is_ok();
                let direct = std::fs::read(secret2.join("key.pem")).is_ok();
                std::fs::write(own2.join("new.txt"), "x").unwrap();
                let uid = std::fs::metadata(own2.join("new.txt")).unwrap().uid();
                (through, direct, uid)
            }))
            .unwrap();
        assert!(!through_link, "read through a symlink into a root-only dir");
        assert!(!direct);
        assert_eq!(written_uid, ID.uid, "files are created as the server");
        // The blocking thread is back to root afterwards.
        assert!(rt
            .block_on(run_blocking(None, move || std::fs::read(
                secret.join("key.pem")
            )
            .is_ok()))
            .unwrap());
        std::fs::remove_dir_all(&d).unwrap();
    }

    #[test]
    fn run_async_covers_tokio_fs() {
        if !is_root() {
            return;
        }
        let d = scratch();
        let secret = d.join("secret.txt");
        std::fs::write(&secret, "s").unwrap();
        std::fs::set_permissions(&secret, std::fs::Permissions::from_mode(0o600)).unwrap();
        let rt = tokio::runtime::Runtime::new().unwrap();
        let s2 = secret.clone();
        let denied = rt
            .block_on(run_async(Some(ID), async move {
                tokio::fs::read(&s2).await.is_err()
            }))
            .unwrap();
        assert!(denied);
        std::fs::remove_dir_all(&d).unwrap();
    }
}
