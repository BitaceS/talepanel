//! Auto-restart policy: decides whether a server that just exited is started
//! again, and after how long.
//!
//! Pure logic (no I/O, the clock is passed in) so the decision table is unit
//! tested; `ProcessManager` acts on the result.

use std::collections::VecDeque;
use std::time::{Duration, Instant};

use super::hytale::ServerConfig;

/// Restarts allowed within the window when the panel sends no crash limit.
pub const DEFAULT_MAX_RESTARTS: u32 = 3;
/// Window over which crashes are counted when the panel sends none.
pub const DEFAULT_WINDOW: Duration = Duration::from_secs(600);
/// Delay before the first restart; doubles per crash in the window.
const BACKOFF_BASE: Duration = Duration::from_secs(5);
const BACKOFF_MAX: Duration = Duration::from_secs(60);

/// How a server process ended.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ProcessExit {
    /// It exited after a stop, restart or kill from the panel.
    Requested,
    /// It exited on its own. `None` = killed by a signal (e.g. the OOM killer).
    Exited(Option<i32>),
}

/// Why a server is not restarted.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum NoRestartReason {
    /// Auto restart is off for this server.
    Disabled,
    /// The panel asked for the stop.
    Requested,
    /// Exit code 0: the server shut down normally (e.g. `stop` typed in the console).
    CleanExit,
    /// Hytale exit codes 8 (staged self-update) and 11 (data inside the launcher
    /// install folder): restarting cannot help, the server needs an update or a
    /// config fix first.
    NeedsAttention(i32),
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum RestartDecision {
    /// Start the server again after `delay`. `attempt` counts the crashes in
    /// the current window (1 = first crash).
    Restart { attempt: u32, delay: Duration },
    /// Too many crashes within the window: leave the server crashed.
    GiveUp { crashes: u32 },
    NoRestart(NoRestartReason),
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct RestartPolicy {
    pub enabled: bool,
    /// Automatic restarts allowed within `window`; the next crash gives up.
    pub max_restarts: u32,
    pub window: Duration,
}

impl RestartPolicy {
    pub fn from_config(cfg: &ServerConfig) -> Self {
        Self {
            enabled: cfg.auto_restart,
            max_restarts: if cfg.crash_limit == 0 { DEFAULT_MAX_RESTARTS } else { cfg.crash_limit },
            window: if cfg.crash_window_s == 0 {
                DEFAULT_WINDOW
            } else {
                Duration::from_secs(u64::from(cfg.crash_window_s))
            },
        }
    }
}

/// Exit codes after which a restart is pointless (see `NoRestartReason::NeedsAttention`).
pub fn exit_needs_attention(code: i32) -> bool {
    matches!(code, 8 | 11)
}

/// Delay before restart number `attempt` (1-based): 5s, 10s, 20s, 40s, then 60s.
pub fn backoff(attempt: u32) -> Duration {
    let shift = attempt.saturating_sub(1).min(16);
    BACKOFF_BASE.saturating_mul(1u32 << shift).min(BACKOFF_MAX)
}

/// Crash timestamps of one server, oldest first.
#[derive(Debug, Default)]
pub struct CrashHistory {
    crashes: VecDeque<Instant>,
}

impl CrashHistory {
    /// Decide what to do about an exit at `now`. Only crashes are recorded.
    pub fn decide(&mut self, policy: &RestartPolicy, exit: ProcessExit, now: Instant) -> RestartDecision {
        let code = match exit {
            ProcessExit::Requested => return RestartDecision::NoRestart(NoRestartReason::Requested),
            ProcessExit::Exited(code) => code,
        };
        match code {
            Some(0) => return RestartDecision::NoRestart(NoRestartReason::CleanExit),
            Some(c) if exit_needs_attention(c) => {
                return RestartDecision::NoRestart(NoRestartReason::NeedsAttention(c))
            }
            _ => {}
        }
        if !policy.enabled {
            return RestartDecision::NoRestart(NoRestartReason::Disabled);
        }

        while self
            .crashes
            .front()
            .is_some_and(|t| now.saturating_duration_since(*t) >= policy.window)
        {
            self.crashes.pop_front();
        }
        self.crashes.push_back(now);

        let crashes = self.crashes.len() as u32;
        if crashes > policy.max_restarts {
            RestartDecision::GiveUp { crashes }
        } else {
            RestartDecision::Restart { attempt: crashes, delay: backoff(crashes) }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn policy() -> RestartPolicy {
        RestartPolicy { enabled: true, max_restarts: 3, window: Duration::from_secs(300) }
    }

    fn crash(code: i32) -> ProcessExit {
        ProcessExit::Exited(Some(code))
    }

    #[test]
    fn restarts_a_crash_with_backoff() {
        let mut h = CrashHistory::default();
        let t = Instant::now();
        assert_eq!(
            h.decide(&policy(), crash(1), t),
            RestartDecision::Restart { attempt: 1, delay: Duration::from_secs(5) }
        );
        assert_eq!(
            h.decide(&policy(), crash(1), t + Duration::from_secs(30)),
            RestartDecision::Restart { attempt: 2, delay: Duration::from_secs(10) }
        );
        assert_eq!(
            h.decide(&policy(), crash(1), t + Duration::from_secs(60)),
            RestartDecision::Restart { attempt: 3, delay: Duration::from_secs(20) }
        );
    }

    #[test]
    fn gives_up_after_the_crash_limit_within_the_window() {
        let mut h = CrashHistory::default();
        let t = Instant::now();
        for i in 0..3 {
            assert!(matches!(
                h.decide(&policy(), crash(1), t + Duration::from_secs(i * 10)),
                RestartDecision::Restart { .. }
            ));
        }
        assert_eq!(
            h.decide(&policy(), crash(1), t + Duration::from_secs(40)),
            RestartDecision::GiveUp { crashes: 4 }
        );
    }

    #[test]
    fn crashes_outside_the_window_are_forgotten() {
        let mut h = CrashHistory::default();
        let t = Instant::now();
        for i in 0..3 {
            h.decide(&policy(), crash(1), t + Duration::from_secs(i));
        }
        // Six minutes later the earlier crashes no longer count.
        assert_eq!(
            h.decide(&policy(), crash(1), t + Duration::from_secs(360)),
            RestartDecision::Restart { attempt: 1, delay: Duration::from_secs(5) }
        );
    }

    #[test]
    fn signal_kills_count_as_crashes() {
        let mut h = CrashHistory::default();
        assert!(matches!(
            h.decide(&policy(), ProcessExit::Exited(None), Instant::now()),
            RestartDecision::Restart { attempt: 1, .. }
        ));
    }

    #[test]
    fn user_stops_and_clean_exits_are_not_restarted() {
        let mut h = CrashHistory::default();
        let t = Instant::now();
        assert_eq!(
            h.decide(&policy(), ProcessExit::Requested, t),
            RestartDecision::NoRestart(NoRestartReason::Requested)
        );
        assert_eq!(h.decide(&policy(), crash(0), t), RestartDecision::NoRestart(NoRestartReason::CleanExit));
        // Neither was recorded as a crash.
        assert!(matches!(h.decide(&policy(), crash(1), t), RestartDecision::Restart { attempt: 1, .. }));
    }

    #[test]
    fn update_and_install_dir_exit_codes_are_not_restarted() {
        let mut h = CrashHistory::default();
        let t = Instant::now();
        assert_eq!(h.decide(&policy(), crash(8), t), RestartDecision::NoRestart(NoRestartReason::NeedsAttention(8)));
        assert_eq!(h.decide(&policy(), crash(11), t), RestartDecision::NoRestart(NoRestartReason::NeedsAttention(11)));
        assert!(matches!(h.decide(&policy(), crash(1), t), RestartDecision::Restart { attempt: 1, .. }));
    }

    #[test]
    fn disabled_policy_never_restarts() {
        let mut h = CrashHistory::default();
        let off = RestartPolicy { enabled: false, ..policy() };
        assert_eq!(
            h.decide(&off, crash(1), Instant::now()),
            RestartDecision::NoRestart(NoRestartReason::Disabled)
        );
    }

    #[test]
    fn backoff_doubles_and_caps() {
        assert_eq!(backoff(1), Duration::from_secs(5));
        assert_eq!(backoff(4), Duration::from_secs(40));
        assert_eq!(backoff(5), Duration::from_secs(60));
        assert_eq!(backoff(1000), Duration::from_secs(60));
        assert_eq!(backoff(0), Duration::from_secs(5));
    }

    #[test]
    fn policy_defaults_apply_to_missing_panel_values() {
        let cfg: ServerConfig = serde_json::from_str(
            r#"{"server_id":"s","name":"n","data_path":"/d","port":5520,
                "ram_limit_mb":4096,"cpu_limit":0}"#,
        )
        .unwrap();
        let p = RestartPolicy::from_config(&cfg);
        assert!(!p.enabled, "older panels send no auto_restart: keep the old behaviour");
        assert_eq!(p.max_restarts, DEFAULT_MAX_RESTARTS);
        assert_eq!(p.window, DEFAULT_WINDOW);

        let cfg: ServerConfig = serde_json::from_str(
            r#"{"server_id":"s","name":"n","data_path":"/d","port":5520,"ram_limit_mb":4096,
                "cpu_limit":150,"auto_restart":true,"crash_limit":5,"crash_window_s":120}"#,
        )
        .unwrap();
        let p = RestartPolicy::from_config(&cfg);
        assert!(p.enabled);
        assert_eq!(p.max_restarts, 5);
        assert_eq!(p.window, Duration::from_secs(120));
    }
}
