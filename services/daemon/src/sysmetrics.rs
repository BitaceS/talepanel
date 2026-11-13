//! Long-lived sysinfo sampler.
//!
//! sysinfo computes CPU usage as the difference between two refreshes of the
//! same `System`. A fresh `System` per call therefore yields 0 % for every
//! process (no previous sample) and, for the host, the average since boot.
//! One shared `System` that is refreshed on every call gives the usage since
//! the previous call instead, i.e. over the heartbeat / metrics interval.

use std::sync::{Mutex, OnceLock};

use sysinfo::{Pid, ProcessRefreshKind, System};

fn shared() -> &'static Mutex<System> {
    static SYS: OnceLock<Mutex<System>> = OnceLock::new();
    SYS.get_or_init(|| Mutex::new(System::new()))
}

/// CPU usage (percent of one core, may exceed 100 on several cores) since the
/// previous call for this PID, and resident memory in MB. The first call for
/// a PID reports 0 % CPU. `(0.0, 0)` when the process does not exist.
/// Blocking; call it from `spawn_blocking`.
pub fn process_usage(pid: u32) -> (f32, u64) {
    let mut sys = shared().lock().unwrap_or_else(|e| e.into_inner());
    let spid = Pid::from(pid as usize);
    if !sys.refresh_process_specifics(spid, ProcessRefreshKind::new().with_cpu().with_memory()) {
        return (0.0, 0);
    }
    match sys.process(spid) {
        Some(p) => (p.cpu_usage(), crate::heartbeat::bytes_to_mb(p.memory())),
        None => (0.0, 0),
    }
}

/// Host CPU usage since the previous call (percent over all cores) and used
/// memory in MB. Blocking; call it from `spawn_blocking`.
pub fn host_usage() -> (f32, u64) {
    let mut sys = shared().lock().unwrap_or_else(|e| e.into_inner());
    sys.refresh_cpu_usage();
    sys.refresh_memory();
    (
        sys.global_cpu_info().cpu_usage(),
        crate::heartbeat::bytes_to_mb(sys.used_memory()),
    )
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn process_cpu_is_measured_between_calls() {
        let pid = std::process::id();
        let _ = process_usage(pid);
        // Burn CPU on this thread for a while so the second sample sees it.
        let start = std::time::Instant::now();
        let mut x: u64 = 1;
        while start.elapsed() < std::time::Duration::from_millis(400) {
            x = x.wrapping_mul(6364136223846793005).wrapping_add(1442695040888963407);
        }
        std::hint::black_box(x);
        let (cpu, ram) = process_usage(pid);
        assert!(cpu > 1.0, "expected measurable CPU for a busy process, got {cpu}");
        assert!(ram > 0, "resident memory should be reported");
    }

    #[test]
    fn unknown_pid_reports_zero() {
        assert_eq!(process_usage(u32::MAX - 7), (0.0, 0));
    }
}
