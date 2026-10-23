//! Per-server CPU limits through cgroup v2 `cpu.max`.
//!
//! The daemon works inside its own cgroup (the systemd service's or the
//! container's). On first use it moves its own processes into a `daemon`
//! leaf, enables the `cpu` controller for its children and then gives every
//! CPU-limited server a `server-<id>` child cgroup:
//!
//! ```text
//! <daemon cgroup>/
//!   daemon/            the daemon itself (and unlimited servers)
//!   server-<uuid>/     cpu.max = "<quota> 100000"
//! ```
//!
//! If that is impossible (no cgroup v2, read-only cgroupfs as in an
//! unprivileged Docker container, controller not delegated) the daemon logs one
//! warning and starts servers without a limit. A limit never fails a start.

use std::io::{ErrorKind, Write};
use std::path::{Path, PathBuf};
use std::sync::OnceLock;

use tracing::{info, warn};

const CGROUP_MOUNT: &str = "/sys/fs/cgroup";
/// CFS period for `cpu.max`, in microseconds (the kernel default).
pub const CPU_PERIOD_US: u64 = 100_000;
/// Smallest quota the kernel accepts.
const MIN_QUOTA_US: u64 = 1_000;
const DAEMON_LEAF: &str = "daemon";
/// Linux errno "no such process" (libc is a unix-only dependency here).
const ESRCH: i32 = 3;

/// `cpu.max` content for a limit in percent of one core, `None` = unlimited.
pub fn cpu_max_value(percent: f32) -> Option<String> {
    if !percent.is_finite() || percent <= 0.0 {
        return None;
    }
    let quota = (f64::from(percent) * CPU_PERIOD_US as f64 / 100.0).round() as u64;
    Some(format!("{} {}", quota.max(MIN_QUOTA_US), CPU_PERIOD_US))
}

/// The daemon's own cgroup path from `/proc/self/cgroup` (the v2 `0::` line).
fn parse_own_cgroup(proc_self_cgroup: &str) -> Option<String> {
    proc_self_cgroup
        .lines()
        .find_map(|l| l.strip_prefix("0::"))
        .map(|p| p.trim().to_string())
        .filter(|p| p.starts_with('/'))
}

/// Child cgroup name for a server; `None` if the id is not a plain UUID-like
/// token (it becomes a path component).
fn server_cgroup_name(server_id: &str) -> Option<String> {
    let ok = !server_id.is_empty()
        && server_id.len() <= 64
        && server_id.bytes().all(|b| b.is_ascii_alphanumeric() || b == b'-');
    ok.then(|| format!("server-{server_id}"))
}

fn has_controller(list: &str, name: &str) -> bool {
    list.split_whitespace().any(|c| c == name)
}

fn write_file(path: &Path, value: &str) -> std::io::Result<()> {
    // cgroupfs files must be written with a single write(2) and without O_TRUNC.
    let mut f = std::fs::OpenOptions::new().write(true).open(path)?;
    f.write_all(value.as_bytes())
}

fn create_dir(path: &Path) -> std::io::Result<()> {
    match std::fs::create_dir(path) {
        Err(e) if e.kind() == ErrorKind::AlreadyExists => Ok(()),
        r => r,
    }
}

/// Set up the delegated hierarchy below `base` (the daemon's own cgroup).
fn prepare(base: &Path) -> Result<(), String> {
    let read = |name: &str| {
        std::fs::read_to_string(base.join(name))
            .map_err(|e| format!("cannot read {}: {e}", base.join(name).display()))
    };

    // Only the real root cgroup lacks cgroup.type. Moving every process out of
    // it would reorganise the whole host, so refuse.
    if !base.join("cgroup.type").exists() {
        return Err("the daemon runs in the root cgroup; run it as a systemd service or in a container".into());
    }
    if !has_controller(&read("cgroup.controllers")?, "cpu") {
        return Err(format!(
            "the cpu controller is not available in {} (systemd: Delegate=yes on the daemon's unit)",
            base.display()
        ));
    }
    if has_controller(&read("cgroup.subtree_control")?, "cpu") {
        return Ok(());
    }

    // cgroup v2 "no internal processes": a cgroup that distributes a
    // controller to children may not hold processes itself.
    let leaf = base.join(DAEMON_LEAF);
    create_dir(&leaf).map_err(|e| format!("cannot create {}: {e}", leaf.display()))?;
    for _ in 0..3 {
        let procs = read("cgroup.procs")?;
        let pids: Vec<&str> = procs.split_whitespace().collect();
        if pids.is_empty() {
            break;
        }
        for pid in pids {
            match write_file(&leaf.join("cgroup.procs"), pid) {
                Ok(()) => {}
                // The process exited meanwhile.
                Err(e) if e.raw_os_error() == Some(ESRCH) => {}
                Err(e) => return Err(format!("cannot move process {pid} into {}: {e}", leaf.display())),
            }
        }
    }
    write_file(&base.join("cgroup.subtree_control"), "+cpu")
        .map_err(|e| format!("cannot enable the cpu controller in {}: {e}", base.display()))
}

/// The prepared base cgroup, or why CPU limits cannot be enforced. Computed
/// once; the failure is logged once.
fn hierarchy() -> &'static Result<PathBuf, String> {
    static HIERARCHY: OnceLock<Result<PathBuf, String>> = OnceLock::new();
    HIERARCHY.get_or_init(|| {
        let mount = Path::new(CGROUP_MOUNT);
        let result = (|| {
            if !mount.join("cgroup.controllers").exists() {
                return Err(format!("cgroup v2 is not mounted at {CGROUP_MOUNT}"));
            }
            let own = std::fs::read_to_string("/proc/self/cgroup")
                .ok()
                .and_then(|s| parse_own_cgroup(&s))
                .ok_or("cannot determine the daemon's cgroup from /proc/self/cgroup")?;
            let base = mount.join(own.trim_start_matches('/'));
            prepare(&base)?;
            Ok(base)
        })();
        match &result {
            Ok(base) => info!(cgroup = %base.display(), "Per-server CPU limits enabled (cgroup v2)"),
            Err(why) => warn!(
                reason = %why,
                "CPU limits cannot be enforced on this node; servers start without them"
            ),
        }
        result
    })
}

/// Result of applying a server's CPU limit.
#[derive(Debug)]
pub enum CpuLimit {
    /// No limit configured.
    Unlimited,
    /// The process is in this cgroup with `cpu.max` set.
    Applied(PathBuf),
    /// A limit is configured but could not be enforced (reason inside).
    NotEnforced(String),
}

/// Put `pid` into its server's cgroup with the given limit (percent of one core).
pub fn apply_cpu_limit(server_id: &str, percent: f32, pid: u32) -> CpuLimit {
    let Some(value) = cpu_max_value(percent) else {
        return CpuLimit::Unlimited;
    };
    if pid == 0 {
        return CpuLimit::NotEnforced("process id unknown".into());
    }
    let base = match hierarchy() {
        Ok(b) => b,
        Err(why) => return CpuLimit::NotEnforced(why.clone()),
    };
    let Some(name) = server_cgroup_name(server_id) else {
        return CpuLimit::NotEnforced("server id is not a valid cgroup name".into());
    };
    let dir = base.join(name);
    let result = create_dir(&dir)
        .and_then(|()| write_file(&dir.join("cpu.max"), &value))
        .and_then(|()| write_file(&dir.join("cgroup.procs"), &pid.to_string()));
    match result {
        Ok(()) => CpuLimit::Applied(dir),
        Err(e) => {
            warn!(%server_id, cgroup = %dir.display(), error = %e, "Failed to apply CPU limit");
            CpuLimit::NotEnforced(format!("cannot configure {}: {e}", dir.display()))
        }
    }
}

/// Remove a server cgroup after its process exited (best effort; the kernel
/// only allows removing an empty cgroup).
pub fn remove_server_cgroup(dir: &Path) {
    if let Err(e) = std::fs::remove_dir(dir) {
        if e.kind() != ErrorKind::NotFound {
            warn!(cgroup = %dir.display(), error = %e, "Failed to remove server cgroup");
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn percent_maps_to_quota_per_100ms_period() {
        assert_eq!(cpu_max_value(100.0).as_deref(), Some("100000 100000"));
        assert_eq!(cpu_max_value(250.0).as_deref(), Some("250000 100000"));
        assert_eq!(cpu_max_value(50.0).as_deref(), Some("50000 100000"));
        // Below the kernel minimum the quota is clamped, not rejected.
        assert_eq!(cpu_max_value(0.5).as_deref(), Some("1000 100000"));
    }

    #[test]
    fn zero_negative_and_garbage_mean_unlimited() {
        assert_eq!(cpu_max_value(0.0), None);
        assert_eq!(cpu_max_value(-5.0), None);
        assert_eq!(cpu_max_value(f32::NAN), None);
        assert_eq!(cpu_max_value(f32::INFINITY), None);
    }

    #[test]
    fn own_cgroup_comes_from_the_v2_line() {
        assert_eq!(
            parse_own_cgroup("0::/system.slice/taledaemon.service\n").as_deref(),
            Some("/system.slice/taledaemon.service")
        );
        assert_eq!(parse_own_cgroup("0::/\n").as_deref(), Some("/"));
        // cgroup v1 only: no unified line.
        assert_eq!(parse_own_cgroup("12:cpu,cpuacct:/docker/abc\n"), None);
    }

    #[test]
    fn server_ids_must_be_safe_path_components() {
        assert_eq!(
            server_cgroup_name("123e4567-e89b-12d3-a456-426614174000").as_deref(),
            Some("server-123e4567-e89b-12d3-a456-426614174000")
        );
        assert_eq!(server_cgroup_name("../../etc"), None);
        assert_eq!(server_cgroup_name("a/b"), None);
        assert_eq!(server_cgroup_name(""), None);
    }

    fn fake_cgroup(tag: &str, controllers: &str) -> PathBuf {
        let dir = std::env::temp_dir().join(format!("tp-cgroup-test-{tag}-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        for (f, v) in [
            ("cgroup.type", "domain\n"),
            ("cgroup.controllers", controllers),
            ("cgroup.subtree_control", ""),
            ("cgroup.procs", "4242\n"),
        ] {
            std::fs::write(dir.join(f), v).unwrap();
        }
        dir
    }

    #[test]
    fn prepare_moves_processes_to_a_leaf_and_enables_cpu() {
        let base = fake_cgroup("ok", "cpuset cpu io memory pids\n");
        std::fs::create_dir_all(base.join(DAEMON_LEAF)).unwrap();
        std::fs::write(base.join(DAEMON_LEAF).join("cgroup.procs"), "").unwrap();
        prepare(&base).unwrap();
        assert_eq!(std::fs::read_to_string(base.join("daemon/cgroup.procs")).unwrap(), "4242");
        assert_eq!(std::fs::read_to_string(base.join("cgroup.subtree_control")).unwrap(), "+cpu");
        let _ = std::fs::remove_dir_all(&base);
    }

    #[test]
    fn prepare_refuses_without_a_delegated_cpu_controller() {
        let base = fake_cgroup("nocpu", "cpuset io memory pids\n");
        let err = prepare(&base).unwrap_err();
        assert!(err.contains("Delegate=yes"), "{err}");
        let _ = std::fs::remove_dir_all(&base);
    }

    #[test]
    fn prepare_refuses_the_root_cgroup() {
        let base = fake_cgroup("root", "cpu\n");
        std::fs::remove_file(base.join("cgroup.type")).unwrap();
        assert!(prepare(&base).unwrap_err().contains("root cgroup"));
        let _ = std::fs::remove_dir_all(&base);
    }

    /// Runs against the real cgroupfs. Needs a writable cgroup v2 (e.g.
    /// `docker run --privileged --cgroupns=private ...`), so it is ignored by default.
    #[test]
    #[ignore]
    fn real_cgroup_throttles_a_busy_loop() {
        let mut child = std::process::Command::new("sh")
            .args(["-c", "while :; do :; done"])
            .spawn()
            .unwrap();
        let pid = child.id();
        let dir = match apply_cpu_limit("00000000-0000-0000-0000-00000000c0de", 20.0, pid) {
            CpuLimit::Applied(dir) => dir,
            other => {
                let _ = child.kill();
                panic!("limit not applied: {other:?}");
            }
        };
        assert_eq!(std::fs::read_to_string(dir.join("cpu.max")).unwrap().trim(), "20000 100000");
        let usage = |d: &Path| -> u64 {
            let stat = std::fs::read_to_string(d.join("cpu.stat")).unwrap();
            stat.lines()
                .find_map(|l| l.strip_prefix("usage_usec "))
                .unwrap()
                .trim()
                .parse()
                .unwrap()
        };
        let before = usage(&dir);
        std::thread::sleep(std::time::Duration::from_secs(2));
        let used = usage(&dir) - before;
        let _ = child.kill();
        let _ = child.wait();
        // 20% of one core over 2s is 400ms; allow some slack.
        assert!(used < 600_000, "used {used}us of CPU in 2s, limit not enforced");
        assert!(used > 100_000, "used {used}us, the loop did not run at all");
        std::thread::sleep(std::time::Duration::from_millis(100));
        remove_server_cgroup(&dir);
        assert!(!dir.exists());
    }
}
