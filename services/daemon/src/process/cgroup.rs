//! Per-server resource limits through cgroup v2: `cpu.max`, `memory.max` and
//! `pids.max`.
//!
//! The daemon works inside its own cgroup (the systemd service's or the
//! container's). On first use it moves its own processes into a `daemon`
//! leaf, enables the `cpu`, `memory` and `pids` controllers it has for its
//! children and then gives every server a `server-<id>` child cgroup:
//!
//! ```text
//! <daemon cgroup>/
//!   daemon/            the daemon itself
//!   server-<uuid>/     cpu.max, memory.max, pids.max
//! ```
//!
//! If that is impossible (no cgroup v2, read-only cgroupfs as in an
//! unprivileged Docker container, controller not delegated) the daemon logs one
//! warning and starts servers without the missing limits. A limit never fails
//! a start.

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
/// Controllers the daemon uses, in the order it enables them.
const CONTROLLERS: [&str; 3] = ["cpu", "memory", "pids"];
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

/// `memory.max` for a server whose JVM heap is capped at `heap_mb`, in MiB;
/// `None` = no limit. The JVM needs memory beyond the heap (metaspace, thread
/// stacks, code cache, direct buffers, the garbage collector's own tables),
/// so the cap is the heap plus half of it, at least 512 MiB more. Only a
/// runaway process reaches it; the kernel then kills the server, which is
/// reported as a crash.
pub fn memory_max_mb(heap_mb: u32) -> Option<u64> {
    (heap_mb > 0).then(|| u64::from(heap_mb) + (u64::from(heap_mb) / 2).max(512))
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
        && server_id
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || b == b'-');
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
/// Returns the controllers available to server cgroups.
fn prepare(base: &Path) -> Result<Vec<&'static str>, String> {
    let read = |name: &str| {
        std::fs::read_to_string(base.join(name))
            .map_err(|e| format!("cannot read {}: {e}", base.join(name).display()))
    };

    // Only the real root cgroup lacks cgroup.type. Moving every process out of
    // it would reorganise the whole host, so refuse.
    if !base.join("cgroup.type").exists() {
        return Err(
            "the daemon runs in the root cgroup; run it as a systemd service or in a container"
                .into(),
        );
    }
    let available = read("cgroup.controllers")?;
    let wanted: Vec<&'static str> = CONTROLLERS
        .into_iter()
        .filter(|c| has_controller(&available, c))
        .collect();
    if wanted.is_empty() {
        return Err(format!(
            "no cpu, memory or pids controller is available in {} (systemd: Delegate=yes on the daemon's unit)",
            base.display()
        ));
    }
    let enabled = read("cgroup.subtree_control")?;
    if wanted.iter().all(|c| has_controller(&enabled, c)) {
        return Ok(wanted);
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
                Err(e) => {
                    return Err(format!(
                        "cannot move process {pid} into {}: {e}",
                        leaf.display()
                    ))
                }
            }
        }
    }
    let control = base.join("cgroup.subtree_control");
    let all: Vec<String> = wanted.iter().map(|c| format!("+{c}")).collect();
    if write_file(&control, &all.join(" ")).is_ok() {
        return Ok(wanted);
    }
    // One controller may be refused (e.g. memory in some nested setups);
    // keep the others.
    let mut ok = Vec::new();
    let mut last_err = None;
    for c in wanted {
        match write_file(&control, &format!("+{c}")) {
            Ok(()) => ok.push(c),
            Err(e) => last_err = Some(format!("{c}: {e}")),
        }
    }
    if ok.is_empty() {
        return Err(format!(
            "cannot enable controllers in {}: {}",
            base.display(),
            last_err.unwrap_or_default()
        ));
    }
    Ok(ok)
}

/// The prepared base cgroup, or why CPU limits cannot be enforced. Computed
/// once; the failure is logged once.
fn hierarchy() -> &'static Result<(PathBuf, Vec<&'static str>), String> {
    static HIERARCHY: OnceLock<Result<(PathBuf, Vec<&'static str>), String>> = OnceLock::new();
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
            let controllers = prepare(&base)?;
            Ok((base, controllers))
        })();
        match &result {
            Ok((base, controllers)) => {
                info!(cgroup = %base.display(), controllers = ?controllers, "Per-server resource limits enabled (cgroup v2)")
            }
            Err(why) => warn!(
                reason = %why,
                "Resource limits (CPU, memory, tasks) cannot be enforced on this node; servers start without them"
            ),
        }
        result
    })
}

/// Limits for one server; zero/`None` means none.
#[derive(Debug, Clone, Copy, Default)]
pub struct Limits {
    /// Percent of one core.
    pub cpu_percent: f32,
    pub memory_max_mb: Option<u64>,
    pub pids_max: Option<u32>,
}

/// What [`apply_limits`] managed to set.
#[derive(Debug)]
pub struct Applied {
    pub dir: PathBuf,
    pub cpu: bool,
    pub memory: bool,
    pub pids: bool,
    /// Requested limits that are not enforced, with the reason.
    pub missing: Vec<String>,
}

/// Result of applying a server's limits.
#[derive(Debug)]
pub enum LimitOutcome {
    /// No limit configured.
    Unlimited,
    /// The process is in this cgroup with (some of) the limits set.
    Applied(Applied),
    /// Limits are configured but none could be enforced (reason inside).
    NotEnforced(String),
}

/// Put `pid` into its server's cgroup with the given limits.
pub fn apply_limits(server_id: &str, limits: Limits, pid: u32) -> LimitOutcome {
    let cpu = cpu_max_value(limits.cpu_percent);
    let memory = limits.memory_max_mb.filter(|m| *m > 0);
    let pids = limits.pids_max.filter(|p| *p > 0);
    if cpu.is_none() && memory.is_none() && pids.is_none() {
        return LimitOutcome::Unlimited;
    }
    if pid == 0 {
        return LimitOutcome::NotEnforced("process id unknown".into());
    }
    let (base, controllers) = match hierarchy() {
        Ok(h) => h,
        Err(why) => return LimitOutcome::NotEnforced(why.clone()),
    };
    let Some(name) = server_cgroup_name(server_id) else {
        return LimitOutcome::NotEnforced("server id is not a valid cgroup name".into());
    };
    let dir = base.join(name);
    if let Err(e) = create_dir(&dir) {
        return LimitOutcome::NotEnforced(format!("cannot create {}: {e}", dir.display()));
    }

    let mut applied = Applied {
        dir: dir.clone(),
        cpu: false,
        memory: false,
        pids: false,
        missing: Vec::new(),
    };
    let mut set = |controller: &str, file: &str, value: String| -> bool {
        if !controllers.contains(&controller) {
            applied
                .missing
                .push(format!("{controller}: controller not available"));
            return false;
        }
        match write_file(&dir.join(file), &value) {
            Ok(()) => true,
            Err(e) => {
                applied.missing.push(format!("{controller}: {e}"));
                false
            }
        }
    };
    let cpu_ok = cpu.is_some_and(|v| set("cpu", "cpu.max", v));
    let memory_ok = memory.is_some_and(|m| {
        let ok = set("memory", "memory.max", (m * 1024 * 1024).to_string());
        if ok {
            // Kill the whole server on OOM rather than one random thread's
            // process; best effort (older kernels lack the file).
            let _ = write_file(&dir.join("memory.oom.group"), "1");
        }
        ok
    });
    let pids_ok = pids.is_some_and(|p| set("pids", "pids.max", p.to_string()));
    applied.cpu = cpu_ok;
    applied.memory = memory_ok;
    applied.pids = pids_ok;

    if !(cpu_ok || memory_ok || pids_ok) {
        remove_server_cgroup(&dir);
        return LimitOutcome::NotEnforced(applied.missing.join("; "));
    }
    if let Err(e) = write_file(&dir.join("cgroup.procs"), &pid.to_string()) {
        warn!(%server_id, cgroup = %dir.display(), error = %e, "Failed to move the server into its cgroup");
        remove_server_cgroup(&dir);
        return LimitOutcome::NotEnforced(format!("cannot configure {}: {e}", dir.display()));
    }
    LimitOutcome::Applied(applied)
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
    fn prepare_moves_processes_to_a_leaf_and_enables_controllers() {
        let base = fake_cgroup("ok", "cpuset cpu io memory pids\n");
        std::fs::create_dir_all(base.join(DAEMON_LEAF)).unwrap();
        std::fs::write(base.join(DAEMON_LEAF).join("cgroup.procs"), "").unwrap();
        assert_eq!(prepare(&base).unwrap(), vec!["cpu", "memory", "pids"]);
        assert_eq!(
            std::fs::read_to_string(base.join("daemon/cgroup.procs")).unwrap(),
            "4242"
        );
        assert_eq!(
            std::fs::read_to_string(base.join("cgroup.subtree_control")).unwrap(),
            "+cpu +memory +pids"
        );
        let _ = std::fs::remove_dir_all(&base);
    }

    #[test]
    fn prepare_uses_the_controllers_that_exist() {
        let base = fake_cgroup("nocpu", "cpuset io memory pids\n");
        std::fs::create_dir_all(base.join(DAEMON_LEAF)).unwrap();
        std::fs::write(base.join(DAEMON_LEAF).join("cgroup.procs"), "").unwrap();
        assert_eq!(prepare(&base).unwrap(), vec!["memory", "pids"]);
        let _ = std::fs::remove_dir_all(&base);
    }

    #[test]
    fn prepare_refuses_without_any_delegated_controller() {
        let base = fake_cgroup("none", "cpuset io\n");
        let err = prepare(&base).unwrap_err();
        assert!(err.contains("Delegate=yes"), "{err}");
        let _ = std::fs::remove_dir_all(&base);
    }

    #[test]
    fn memory_cap_leaves_room_above_the_heap() {
        assert_eq!(memory_max_mb(0), None);
        assert_eq!(memory_max_mb(512), Some(1024));
        assert_eq!(memory_max_mb(1024), Some(1536));
        assert_eq!(memory_max_mb(4096), Some(6144));
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
        let limits = Limits {
            cpu_percent: 20.0,
            memory_max_mb: Some(256),
            pids_max: Some(64),
        };
        let dir = match apply_limits("00000000-0000-0000-0000-00000000c0de", limits, pid) {
            LimitOutcome::Applied(a) => {
                assert!(a.cpu && a.memory && a.pids, "{:?}", a.missing);
                a.dir
            }
            other => {
                let _ = child.kill();
                panic!("limit not applied: {other:?}");
            }
        };
        assert_eq!(
            std::fs::read_to_string(dir.join("cpu.max")).unwrap().trim(),
            "20000 100000"
        );
        assert_eq!(
            std::fs::read_to_string(dir.join("memory.max"))
                .unwrap()
                .trim(),
            (256u64 * 1024 * 1024).to_string()
        );
        assert_eq!(
            std::fs::read_to_string(dir.join("pids.max"))
                .unwrap()
                .trim(),
            "64"
        );
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
        assert!(
            used < 600_000,
            "used {used}us of CPU in 2s, limit not enforced"
        );
        assert!(used > 100_000, "used {used}us, the loop did not run at all");
        std::thread::sleep(std::time::Duration::from_millis(100));
        remove_server_cgroup(&dir);
        assert!(!dir.exists());
    }
}
