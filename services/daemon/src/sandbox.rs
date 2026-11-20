//! Per-server sandbox (isolation step 2).
//!
//! Step 1 ([`crate::isolation`]) gives every game server its own UID. The
//! sandbox limits what such a server can do on the node beyond file access:
//!
//! * **System calls.** A seccomp filter, applied right before the JVM is
//!   executed, refuses the calls a game server never needs but an attacker
//!   inside one would: debugging other processes (`ptrace`,
//!   `process_vm_*`), mounting and namespace tricks (`mount`, `unshare`,
//!   `setns`, `clone` with namespace flags, the new mount API), kernel
//!   attack surface that keeps producing exploits (`bpf`, `io_uring`,
//!   `userfaultfd`, `perf_event_open`, `keyctl`), and host administration
//!   (modules, `kexec`, `reboot`, swap, clock, hostname). The calls fail with
//!   `EPERM`; `clone3` fails with `ENOSYS` so the C library falls back to
//!   `clone`, whose flags the filter can inspect.
//! * **Network.** An nftables table in the daemon's network namespace lets
//!   server UIDs reach the internet and their players, but not the node
//!   itself: no connections to the daemon's port, to other servers' ports or
//!   to anything else listening locally (DNS stays allowed), and no access to
//!   the cloud metadata service.
//!
//! The sandbox needs step 1 (separate UIDs, `no_new_privs`). `auto` (the
//! default) applies what works and logs the rest; `strict` refuses to start
//! the daemon unless both parts are active; `off` turns it off.

use std::sync::Arc;

use anyhow::{bail, Result};
use serde::{Deserialize, Serialize};
use tracing::{error, info, warn};

/// `server_sandbox` setting.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Mode {
    Auto,
    Strict,
    Off,
}

pub fn parse_mode(value: &str) -> Result<Mode> {
    match value.trim().to_ascii_lowercase().as_str() {
        "" | "auto" => Ok(Mode::Auto),
        "strict" | "on" | "1" | "true" | "yes" => Ok(Mode::Strict),
        "off" | "0" | "false" | "no" | "none" => Ok(Mode::Off),
        other => bail!("server_sandbox must be auto, strict or off, not {other:?}"),
    }
}

/// What is active on this node, reported to the panel.
#[derive(Debug, Clone, Copy, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct Status {
    /// Every server under its own UID (step 1).
    pub uid: bool,
    /// System call filter on server processes.
    pub seccomp: bool,
    /// Servers cannot reach the node's own ports.
    pub network: bool,
}

/// Compiled seccomp filters, applied in order in the child before exec.
pub struct SeccompFilters {
    #[cfg(target_os = "linux")]
    programs: Vec<seccompiler::BpfProgram>,
}

impl std::fmt::Debug for SeccompFilters {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str("SeccompFilters")
    }
}

impl SeccompFilters {
    /// Install the filters on the calling thread.
    ///
    /// Runs between fork and exec: it only issues prctl(2)/seccomp(2) on
    /// memory compiled beforehand, which is async-signal-safe. The caller
    /// must have set `no_new_privs`.
    #[cfg(target_os = "linux")]
    pub fn apply(&self) -> std::io::Result<()> {
        for prog in &self.programs {
            seccompiler::apply_filter(prog).map_err(|e| match e {
                seccompiler::Error::Seccomp(io) => io,
                _ => std::io::Error::from_raw_os_error(libc::EINVAL),
            })?;
        }
        Ok(())
    }

    #[cfg(not(target_os = "linux"))]
    pub fn apply(&self) -> std::io::Result<()> {
        Ok(())
    }
}

/// The node's sandbox state.
#[derive(Debug)]
pub struct Sandbox {
    seccomp: Option<Arc<SeccompFilters>>,
    status: Status,
}

impl Sandbox {
    /// Nothing active (isolation off, sandbox off, tests).
    pub fn disabled(uid: bool) -> Self {
        Self {
            seccomp: None,
            status: Status {
                uid,
                ..Status::default()
            },
        }
    }

    /// Set the sandbox up for servers running in `uid_range` (inclusive).
    /// `isolation` tells whether step 1 is active; without it there is
    /// nothing to attach the sandbox to.
    pub async fn setup(mode: Mode, isolation: bool, uid_range: (u32, u32)) -> Result<Self> {
        if mode == Mode::Off {
            warn!("Server sandbox is OFF (TALEDAEMON_SERVER_SANDBOX=off): no system call filter and no network rules for game servers");
            return Ok(Self::disabled(isolation));
        }
        if !isolation {
            if mode == Mode::Strict {
                bail!("server_sandbox=strict needs per-server isolation (TALEDAEMON_SERVER_ISOLATION)");
            }
            return Ok(Self::disabled(false));
        }

        let seccomp = match compile_seccomp() {
            Ok(f) => Some(Arc::new(f)),
            Err(e) if mode == Mode::Strict => {
                return Err(e.context("server_sandbox=strict: building the seccomp filter"))
            }
            Err(e) => {
                error!(error = %format!("{e:#}"), "Server sandbox: no system call filter on this node");
                None
            }
        };
        let network = match install_network_rules(uid_range).await {
            Ok(()) => true,
            Err(e) if mode == Mode::Strict => {
                return Err(e.context(
                    "server_sandbox=strict: installing the network rules (the daemon needs NET_ADMIN and nftables)",
                ))
            }
            Err(e) => {
                error!(
                    error = %format!("{e:#}"),
                    "Server sandbox: no network rules on this node, game servers can reach the daemon's port \
                     and each other (the daemon container needs the NET_ADMIN capability)"
                );
                false
            }
        };
        let status = Status {
            uid: true,
            seccomp: seccomp.is_some(),
            network,
        };
        info!(
            seccomp = status.seccomp,
            network = status.network,
            "Server sandbox ready"
        );
        Ok(Self { seccomp, status })
    }

    pub fn seccomp(&self) -> Option<Arc<SeccompFilters>> {
        self.seccomp.clone()
    }

    pub fn status(&self) -> Status {
        self.status
    }
}

// ── seccomp ──────────────────────────────────────────────────────────────────

/// Namespace flags `clone` may not carry (CLONE_NEWTIME shares its bit with
/// the exit signal in clone(2) and is refused by the kernel there anyway).
#[cfg(target_os = "linux")]
const CLONE_NAMESPACE_FLAGS: [u64; 7] = [
    libc::CLONE_NEWNS as u64,
    libc::CLONE_NEWCGROUP as u64,
    libc::CLONE_NEWUTS as u64,
    libc::CLONE_NEWIPC as u64,
    libc::CLONE_NEWUSER as u64,
    libc::CLONE_NEWPID as u64,
    libc::CLONE_NEWNET as u64,
];

/// System calls refused outright.
#[cfg(target_os = "linux")]
fn denied_syscalls() -> Vec<i64> {
    let mut v = vec![
        // Other processes' memory.
        libc::SYS_ptrace,
        libc::SYS_process_vm_readv,
        libc::SYS_process_vm_writev,
        // Mounts and namespaces.
        libc::SYS_mount,
        libc::SYS_umount2,
        libc::SYS_pivot_root,
        libc::SYS_chroot,
        libc::SYS_unshare,
        libc::SYS_setns,
        libc::SYS_fsopen,
        libc::SYS_fsconfig,
        libc::SYS_fsmount,
        libc::SYS_fspick,
        libc::SYS_move_mount,
        libc::SYS_open_tree,
        libc::SYS_mount_setattr,
        libc::SYS_open_by_handle_at,
        libc::SYS_name_to_handle_at,
        // Kernel attack surface.
        libc::SYS_bpf,
        libc::SYS_perf_event_open,
        libc::SYS_userfaultfd,
        libc::SYS_io_uring_setup,
        libc::SYS_io_uring_enter,
        libc::SYS_io_uring_register,
        libc::SYS_keyctl,
        libc::SYS_add_key,
        libc::SYS_request_key,
        // Host administration.
        libc::SYS_init_module,
        libc::SYS_finit_module,
        libc::SYS_delete_module,
        libc::SYS_kexec_load,
        libc::SYS_kexec_file_load,
        libc::SYS_reboot,
        libc::SYS_swapon,
        libc::SYS_swapoff,
        libc::SYS_acct,
        libc::SYS_quotactl,
        libc::SYS_syslog,
        libc::SYS_settimeofday,
        libc::SYS_clock_settime,
        libc::SYS_clock_adjtime,
        libc::SYS_adjtimex,
        libc::SYS_sethostname,
        libc::SYS_setdomainname,
        libc::SYS_vhangup,
    ];
    #[cfg(target_arch = "x86_64")]
    v.extend([libc::SYS_iopl, libc::SYS_ioperm, libc::SYS_uselib]);
    v
}

#[cfg(target_os = "linux")]
fn compile_seccomp() -> Result<SeccompFilters> {
    use anyhow::Context;
    use seccompiler::{
        BpfProgram, SeccompAction, SeccompCmpArgLen, SeccompCmpOp, SeccompCondition, SeccompFilter,
        SeccompRule, TargetArch,
    };
    use std::collections::BTreeMap;

    let arch = TargetArch::try_from(std::env::consts::ARCH)
        .map_err(|e| anyhow::anyhow!("unsupported architecture for seccomp: {e:?}"))?;

    let mut rules: BTreeMap<i64, Vec<SeccompRule>> = denied_syscalls()
        .into_iter()
        .map(|nr| (nr, Vec::new()))
        .collect();
    // clone(2) with any namespace flag: one rule per flag, rules are OR'ed.
    let mut clone_rules = Vec::new();
    for flag in CLONE_NAMESPACE_FLAGS {
        let cond = SeccompCondition::new(
            0,
            SeccompCmpArgLen::Qword,
            SeccompCmpOp::MaskedEq(flag),
            flag,
        )
        .context("clone flag condition")?;
        clone_rules.push(SeccompRule::new(vec![cond]).context("clone flag rule")?);
    }
    rules.insert(libc::SYS_clone, clone_rules);

    let deny: BpfProgram = SeccompFilter::new(
        rules,
        SeccompAction::Allow,
        SeccompAction::Errno(libc::EPERM as u32),
        arch,
    )
    .context("building the deny filter")?
    .try_into()
    .context("compiling the deny filter")?;

    // clone3 takes its flags in memory the filter cannot read. ENOSYS makes
    // the C library fall back to clone(2), which the deny filter checks.
    let clone3: BpfProgram = SeccompFilter::new(
        BTreeMap::from([(libc::SYS_clone3, Vec::new())]),
        SeccompAction::Allow,
        SeccompAction::Errno(libc::ENOSYS as u32),
        arch,
    )
    .context("building the clone3 filter")?
    .try_into()
    .context("compiling the clone3 filter")?;

    Ok(SeccompFilters {
        programs: vec![clone3, deny],
    })
}

#[cfg(not(target_os = "linux"))]
fn compile_seccomp() -> Result<SeccompFilters> {
    bail!("seccomp is only available on Linux")
}

// ── network ──────────────────────────────────────────────────────────────────

const NFT_TABLE: &str = "taledaemon_sandbox";

/// The nftables ruleset for servers with UIDs in `lo..=hi`.
///
/// Every packet to one of the node's own addresses (127.0.0.1, but also the
/// container's or host's LAN address) leaves through the loopback interface,
/// so `oifname "lo"` matches "local" without the fib module, which some
/// kernels (Docker Desktop's among them) lack.
///
/// The chain runs at priority -150: after conntrack (-200), so connection
/// state is known, and before destination NAT (-100), so a DNS query to
/// Docker's resolver at 127.0.0.11 is still seen with port 53 rather than
/// the port Docker rewrites it to.
pub fn nft_ruleset(lo: u32, hi: u32) -> String {
    format!(
        "add table inet {t}\n\
         delete table inet {t}\n\
         table inet {t} {{\n\
         \tchain output {{\n\
         \t\ttype filter hook output priority -150; policy accept;\n\
         \t\tmeta skuid != {lo}-{hi} accept\n\
         \t\tct state established,related accept\n\
         \t\toifname \"lo\" meta l4proto {{ tcp, udp }} th dport 53 accept\n\
         \t\toifname \"lo\" meta l4proto tcp counter reject with tcp reset\n\
         \t\toifname \"lo\" counter reject with icmpx type admin-prohibited\n\
         \t\tip daddr 169.254.169.254 counter reject with icmpx type admin-prohibited\n\
         \t\tip6 daddr fd00:ec2::254 counter reject with icmpx type admin-prohibited\n\
         \t}}\n\
         }}\n",
        t = NFT_TABLE
    )
}

async fn install_network_rules((lo, hi): (u32, u32)) -> Result<()> {
    if lo > hi {
        bail!("empty server UID range");
    }
    nft_apply(&nft_ruleset(lo, hi)).await
}

/// Load an nftables script with `nft -f -` (atomically: all or nothing).
pub async fn nft_apply(script: &str) -> Result<()> {
    use anyhow::Context;
    use tokio::io::AsyncWriteExt;

    let mut child = tokio::process::Command::new("nft")
        .args(["-f", "-"])
        .stdin(std::process::Stdio::piped())
        .stdout(std::process::Stdio::null())
        .stderr(std::process::Stdio::piped())
        .spawn()
        .context("running nft (is nftables installed?)")?;
    let mut stdin = child.stdin.take().context("nft stdin")?;
    stdin
        .write_all(script.as_bytes())
        .await
        .context("writing the ruleset to nft")?;
    drop(stdin);
    let out = child.wait_with_output().await.context("waiting for nft")?;
    if !out.status.success() {
        bail!(
            "nft failed ({}): {}",
            out.status,
            String::from_utf8_lossy(&out.stderr).trim()
        );
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn modes_parse() {
        assert_eq!(parse_mode("").unwrap(), Mode::Auto);
        assert_eq!(parse_mode(" Auto ").unwrap(), Mode::Auto);
        assert_eq!(parse_mode("strict").unwrap(), Mode::Strict);
        assert_eq!(parse_mode("off").unwrap(), Mode::Off);
        assert!(parse_mode("maybe").is_err());
    }

    #[test]
    fn ruleset_targets_the_uid_range() {
        let r = nft_ruleset(2_000_000, 2_065_535);
        assert!(r.contains("meta skuid != 2000000-2065535 accept"));
        assert!(r.contains("priority -150"));
        assert!(r.contains("th dport 53 accept"));
        // Re-running replaces the table instead of stacking rules.
        assert!(r.starts_with(
            "add table inet taledaemon_sandbox\ndelete table inet taledaemon_sandbox\n"
        ));
    }

    #[tokio::test]
    async fn off_and_missing_isolation_disable_everything() {
        let s = Sandbox::setup(Mode::Off, true, (1, 2)).await.unwrap();
        assert_eq!(
            s.status(),
            Status {
                uid: true,
                seccomp: false,
                network: false
            }
        );
        assert!(s.seccomp().is_none());
        let s = Sandbox::setup(Mode::Auto, false, (1, 2)).await.unwrap();
        assert_eq!(s.status(), Status::default());
        assert!(Sandbox::setup(Mode::Strict, false, (1, 2)).await.is_err());
    }

    /// Runs `sh -c script` with the filters applied after no_new_privs, as
    /// the daemon does for a server. Returns the exit code.
    #[cfg(target_os = "linux")]
    fn run_filtered(filters: &Arc<SeccompFilters>, program: &str, args: &[&str]) -> i32 {
        use std::os::unix::process::CommandExt;
        let f = Arc::clone(filters);
        let mut cmd = std::process::Command::new(program);
        cmd.args(args)
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null());
        // SAFETY: prctl and the pre-compiled filter install only.
        unsafe {
            cmd.pre_exec(move || {
                if libc::prctl(libc::PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0) != 0 {
                    return Err(std::io::Error::last_os_error());
                }
                f.apply()
            });
        }
        cmd.status().expect("spawn").code().unwrap_or(-1)
    }

    #[cfg(target_os = "linux")]
    fn run_plain(program: &str, args: &[&str]) -> i32 {
        std::process::Command::new(program)
            .args(args)
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .status()
            .map(|s| s.code().unwrap_or(-1))
            .unwrap_or(-1)
    }

    /// Docker's own seccomp profile already refuses most of these calls, so
    /// a check only counts where the command works without the filter. Run
    /// with `--security-opt seccomp=unconfined --cap-add SYS_ADMIN` to
    /// exercise all of them; `checked` makes sure that happened at least once.
    #[cfg(target_os = "linux")]
    #[test]
    fn seccomp_blocks_namespaces_and_mounts_but_not_normal_work() {
        let f = Arc::new(compile_seccomp().unwrap());
        // Ordinary programs, pipes, subshells and forks still work.
        assert_eq!(
            run_filtered(
                &f,
                "sh",
                &[
                    "-c",
                    "echo hi | cat >/dev/null && (true) && ls / >/dev/null"
                ]
            ),
            0
        );
        let mnt = std::env::temp_dir().join(format!("tp-seccomp-{}", std::process::id()));
        std::fs::create_dir_all(&mnt).unwrap();
        let mnt = mnt.to_string_lossy().to_string();
        let cases: [(&str, Vec<&str>); 4] = [
            ("unshare", vec!["-U", "true"]),
            ("unshare", vec!["-n", "true"]),
            ("unshare", vec!["-m", "true"]),
            ("mount", vec!["-t", "tmpfs", "none", mnt.as_str()]),
        ];
        let mut checked = 0;
        for (prog, args) in &cases {
            if run_plain(prog, args) != 0 {
                eprintln!("{prog} {args:?} fails without the filter here; not checked");
                continue;
            }
            if *prog == "mount" {
                let _ = run_plain("umount", &[mnt.as_str()]);
            }
            assert_ne!(
                run_filtered(&f, prog, args),
                0,
                "{prog} {args:?} passed the filter"
            );
            checked += 1;
        }
        let _ = std::fs::remove_dir(&mnt);
        if std::env::var_os("TP_SECCOMP_STRICT_TEST").is_some() {
            assert_eq!(checked, cases.len(), "run unconfined with SYS_ADMIN");
        }
    }

    /// The JVM must start under the filter: it creates threads through
    /// clone3 (refused with ENOSYS, glibc falls back to clone). Only runs
    /// where `java` is installed (the daemon image); skipped elsewhere.
    #[cfg(target_os = "linux")]
    #[test]
    fn seccomp_lets_the_jvm_start() {
        if std::process::Command::new("java")
            .arg("-version")
            .output()
            .is_err()
        {
            eprintln!("java not installed; skipping");
            return;
        }
        let f = Arc::new(compile_seccomp().unwrap());
        assert_eq!(
            run_filtered(&f, "java", &["-Xmx64m", "-XX:+UseG1GC", "-version"]),
            0
        );
        assert_eq!(
            run_filtered(&f, "java", &["-Xmx64m", "-XX:+UseZGC", "-version"]),
            0
        );
    }
}
