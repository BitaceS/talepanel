//! High-performance network traffic monitor for game server DDoS detection.
//!
//! Runs a 1-second sampling loop that reads kernel counters from `/proc`,
//! computes packet-per-second (PPS) and bytes-per-second (BPS) rates, detects
//! flood patterns, and optionally mitigates via nftables rules.

use std::collections::{HashMap, VecDeque};
use std::sync::Arc;

use chrono::{DateTime, Utc};
use serde::Serialize;
use tokio::sync::RwLock;
use tracing::{debug, error, info, warn};

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

/// How many 1-second samples to keep (1 minute of history).
const MAX_SAMPLES: usize = 60;
/// How many threat events to retain.
const MAX_EVENTS: usize = 200;
/// How many per-IP entries to track.
const MAX_IP_ENTRIES: usize = 10_000;

// ── Threat thresholds ──────────────────────────────────────────────────────

/// Global inbound PPS that triggers a warning.
const THRESH_PPS_WARN: f64 = 10_000.0;
/// Global inbound PPS that triggers a high alert.
const THRESH_PPS_HIGH: f64 = 20_000.0;
/// Global inbound PPS that triggers a critical alert.
const THRESH_PPS_CRITICAL: f64 = 50_000.0;
/// PPS spike ratio (current vs 30s avg) to flag as spike.
const THRESH_SPIKE_RATIO: f64 = 5.0;
/// UDP NoPorts PPS — indicates scanning or reflection to closed ports.
const THRESH_NOPORTS_PPS: f64 = 100.0;
/// SYN_RECV count indicating a SYN flood.
const THRESH_SYN_RECV: usize = 20;
/// Per-IP PPS that triggers a single-source alert (nftables per-IP counter).
const THRESH_IP_PPS: f64 = 5_000.0;
/// Per-IP PPS for auto-blacklist.
const THRESH_IP_BLACKLIST: f64 = 10_000.0;
/// Blacklist duration in seconds.
const BLACKLIST_DURATION_SECS: i64 = 300; // 5 minutes

// ---------------------------------------------------------------------------
// Public types (serialised to JSON for the HTTP endpoint)
// ---------------------------------------------------------------------------

#[derive(Clone, Serialize)]
pub struct NetMonitorSnapshot {
    pub current: CurrentRates,
    pub averages: AverageRates,
    pub peaks: PeakRates,
    pub history: Vec<HistorySample>,
    pub interfaces: Vec<InterfaceInfo>,
    pub game_ports: Vec<GamePortInfo>,
    pub threats: ThreatSummary,
    pub mitigation: MitigationStatus,
}

#[derive(Clone, Serialize, Default)]
pub struct CurrentRates {
    pub pps_in: f64,
    pub pps_out: f64,
    pub bps_in: f64,
    pub bps_out: f64,
    pub udp_pps_in: f64,
    pub udp_noports_pps: f64,
    pub total_connections: usize,
}

#[derive(Clone, Serialize, Default)]
pub struct AverageRates {
    pub pps_in_5s: f64,
    pub pps_in_30s: f64,
    pub bps_in_5s: f64,
    pub bps_in_30s: f64,
}

#[derive(Clone, Serialize, Default)]
pub struct PeakRates {
    pub pps_in_30s: f64,
    pub bps_in_30s: f64,
}

#[derive(Clone, Serialize)]
pub struct HistorySample {
    pub ts: i64,
    pub pps_in: f64,
    pub pps_out: f64,
    pub bps_in: f64,
    pub bps_out: f64,
    pub udp_pps: f64,
}

#[derive(Clone, Serialize)]
pub struct InterfaceInfo {
    pub name: String,
    pub rx_bytes: u64,
    pub tx_bytes: u64,
    pub rx_packets: u64,
    pub tx_packets: u64,
    pub pps_in: f64,
    pub pps_out: f64,
    pub bps_in: f64,
    pub bps_out: f64,
}

#[derive(Clone, Serialize)]
pub struct GamePortInfo {
    pub port: u16,
    pub rx_queue: u64,
    pub drops: u64,
    pub active_connections: usize,
}

#[derive(Clone, Serialize)]
pub struct ThreatSummary {
    pub level: String,
    pub events: Vec<ThreatEvent>,
    pub syn_flood_count: usize,
    pub udp_flood_detected: bool,
    pub pps_spike_detected: bool,
    pub distributed_flood: bool,
    pub top_talkers: Vec<TopTalker>,
}

#[derive(Clone, Serialize)]
pub struct ThreatEvent {
    pub timestamp: DateTime<Utc>,
    pub event_type: String,
    pub severity: String,
    pub description: String,
    pub source_ip: Option<String>,
    pub auto_mitigated: bool,
    pub metrics: HashMap<String, f64>,
}

#[derive(Clone, Serialize)]
pub struct TopTalker {
    pub ip: String,
    pub pps: f64,
    pub connections: usize,
    pub severity: String,
    pub blacklisted: bool,
}

#[derive(Clone, Serialize)]
pub struct MitigationStatus {
    pub nft_available: bool,
    pub active: bool,
    pub blacklisted_ips: Vec<BlacklistEntry>,
    pub rate_limit_active: bool,
    pub rules_applied: usize,
}

#[derive(Clone, Serialize)]
pub struct BlacklistEntry {
    pub ip: String,
    pub reason: String,
    pub expires_at: DateTime<Utc>,
    pub pps_at_block: f64,
}

// ---------------------------------------------------------------------------
// Internal types
// ---------------------------------------------------------------------------

#[derive(Clone)]
struct RawCounters {
    timestamp: std::time::Instant,
    interfaces: Vec<IfaceCounters>,
    udp_in_datagrams: u64,
    udp_no_ports: u64,
    udp_in_errors: u64,
}

impl Default for RawCounters {
    fn default() -> Self {
        Self {
            timestamp: std::time::Instant::now(),
            interfaces: Vec::new(),
            udp_in_datagrams: 0,
            udp_no_ports: 0,
            udp_in_errors: 0,
        }
    }
}

#[derive(Clone)]
struct IfaceCounters {
    name: String,
    rx_bytes: u64,
    tx_bytes: u64,
    rx_packets: u64,
    tx_packets: u64,
}

/// Computed rates for one 1-second sample.
#[derive(Clone, Default)]
struct ComputedSample {
    timestamp: DateTime<Utc>,
    // Interface-level totals (excluding loopback)
    pps_in: f64,
    pps_out: f64,
    bps_in: f64,
    bps_out: f64,
    // UDP-specific
    udp_pps_in: f64,
    udp_noports_pps: f64,
    // Per-interface rates
    iface_rates: Vec<InterfaceInfo>,
}

struct MonitorState {
    prev: Option<RawCounters>,
    samples: VecDeque<ComputedSample>,
    events: VecDeque<ThreatEvent>,
    blacklist: HashMap<String, BlacklistEntry>,
    nft_available: bool,
    nft_initialised: bool,
    mitigation_active: bool,
    rules_applied: usize,
}

// ---------------------------------------------------------------------------
// NetMonitor
// ---------------------------------------------------------------------------

#[derive(Clone)]
pub struct NetMonitor {
    state: Arc<RwLock<MonitorState>>,
}

impl NetMonitor {
    /// Create a new monitor (does NOT start the background loop).
    pub fn new() -> Self {
        Self {
            state: Arc::new(RwLock::new(MonitorState {
                prev: None,
                samples: VecDeque::with_capacity(MAX_SAMPLES),
                events: VecDeque::with_capacity(MAX_EVENTS),
                blacklist: HashMap::new(),
                nft_available: false,
                nft_initialised: false,
                mitigation_active: false,
                rules_applied: 0,
            })),
        }
    }

    /// Start the background sampling loop. Runs until the task is cancelled.
    pub async fn run(&self) {
        // Check nftables availability
        let nft_ok = check_nft_available().await;
        {
            let mut st = self.state.write().await;
            st.nft_available = nft_ok;
        }
        if nft_ok {
            info!("nftables available — DDoS mitigation enabled");
            if let Err(e) = init_nft_rules().await {
                warn!(%e, "Failed to initialise nftables rules");
            } else {
                let mut st = self.state.write().await;
                st.nft_initialised = true;
            }
        } else {
            info!("nftables not available — running in detection-only mode");
        }

        let mut interval = tokio::time::interval(tokio::time::Duration::from_secs(1));
        loop {
            interval.tick().await;
            self.tick().await;
        }
    }

    /// Take a snapshot for the HTTP endpoint.
    pub async fn snapshot(&self) -> NetMonitorSnapshot {
        let st = self.state.read().await;

        // Current rates from the latest sample.
        let current = st.samples.back().map(|s| CurrentRates {
            pps_in: s.pps_in,
            pps_out: s.pps_out,
            bps_in: s.bps_in,
            bps_out: s.bps_out,
            udp_pps_in: s.udp_pps_in,
            udp_noports_pps: s.udp_noports_pps,
            total_connections: 0, // filled below
        }).unwrap_or_default();

        // Averages
        let avg_5 = avg_window(&st.samples, 5);
        let avg_30 = avg_window(&st.samples, 30);

        // Peaks
        let peak_pps = st.samples.iter().rev().take(30).map(|s| s.pps_in).fold(0.0f64, f64::max);
        let peak_bps = st.samples.iter().rev().take(30).map(|s| s.bps_in).fold(0.0f64, f64::max);

        // History for chart
        let history: Vec<HistorySample> = st.samples.iter().map(|s| HistorySample {
            ts: s.timestamp.timestamp(),
            pps_in: s.pps_in,
            pps_out: s.pps_out,
            bps_in: s.bps_in,
            bps_out: s.bps_out,
            udp_pps: s.udp_pps_in,
        }).collect();

        // Interfaces from latest sample
        let interfaces = st.samples.back()
            .map(|s| s.iface_rates.clone())
            .unwrap_or_default();

        // Game port info
        let game_ports = read_game_port_info().await;

        // Connections + SYN count
        let (connections, syn_count) = read_connection_summary().await;

        // Threat assessment
        let threat_level = compute_threat_level(&st, &current, syn_count);
        let udp_flood = current.udp_pps_in > THRESH_PPS_WARN;
        let spike = if let (Some(cur), avg30) = (st.samples.back(), avg_30.pps_in) {
            avg30 > 0.0 && cur.pps_in / avg30 > THRESH_SPIKE_RATIO
        } else { false };

        // Top talkers from conntrack (if available)
        let top_talkers = read_top_talkers(&st.blacklist).await;
        let distributed = top_talkers.len() > 50;

        let threats = ThreatSummary {
            level: threat_level.clone(),
            events: st.events.iter().cloned().collect(),
            syn_flood_count: syn_count,
            udp_flood_detected: udp_flood,
            pps_spike_detected: spike,
            distributed_flood: distributed,
            top_talkers,
        };

        let blacklisted: Vec<BlacklistEntry> = st.blacklist.values()
            .filter(|b| b.expires_at > Utc::now())
            .cloned()
            .collect();

        let mitigation = MitigationStatus {
            nft_available: st.nft_available,
            active: st.mitigation_active,
            blacklisted_ips: blacklisted,
            rate_limit_active: st.nft_initialised,
            rules_applied: st.rules_applied,
        };

        let mut current = current;
        current.total_connections = connections;

        NetMonitorSnapshot {
            current,
            averages: avg_5.merge_30(&Avg30s { pps_in: avg_30.pps_in, bps_in: avg_30.bps_in }),
            peaks: PeakRates { pps_in_30s: peak_pps, bps_in_30s: peak_bps },
            history,
            interfaces,
            game_ports,
            threats,
            mitigation,
        }
    }

    // ── Internal tick ──────────────────────────────────────────────────────

    async fn tick(&self) {
        let raw = read_raw_counters().await;
        let mut st = self.state.write().await;

        if let Some(prev) = &st.prev {
            let dt = raw.timestamp.duration_since(prev.timestamp).as_secs_f64().max(0.001);

            // Compute per-interface deltas
            let mut total_pps_in = 0.0f64;
            let mut total_pps_out = 0.0f64;
            let mut total_bps_in = 0.0f64;
            let mut total_bps_out = 0.0f64;
            let mut iface_rates = Vec::new();

            for cur_if in &raw.interfaces {
                if cur_if.name == "lo" { continue; }
                if let Some(prev_if) = prev.interfaces.iter().find(|i| i.name == cur_if.name) {
                    let d_rx_p = cur_if.rx_packets.saturating_sub(prev_if.rx_packets) as f64 / dt;
                    let d_tx_p = cur_if.tx_packets.saturating_sub(prev_if.tx_packets) as f64 / dt;
                    let d_rx_b = cur_if.rx_bytes.saturating_sub(prev_if.rx_bytes) as f64 / dt;
                    let d_tx_b = cur_if.tx_bytes.saturating_sub(prev_if.tx_bytes) as f64 / dt;

                    total_pps_in += d_rx_p;
                    total_pps_out += d_tx_p;
                    total_bps_in += d_rx_b;
                    total_bps_out += d_tx_b;

                    iface_rates.push(InterfaceInfo {
                        name: cur_if.name.clone(),
                        rx_bytes: cur_if.rx_bytes,
                        tx_bytes: cur_if.tx_bytes,
                        rx_packets: cur_if.rx_packets,
                        tx_packets: cur_if.tx_packets,
                        pps_in: d_rx_p,
                        pps_out: d_tx_p,
                        bps_in: d_rx_b,
                        bps_out: d_tx_b,
                    });
                }
            }

            // UDP-specific deltas
            let udp_pps = raw.udp_in_datagrams.saturating_sub(prev.udp_in_datagrams) as f64 / dt;
            let noports_pps = raw.udp_no_ports.saturating_sub(prev.udp_no_ports) as f64 / dt;

            let sample = ComputedSample {
                timestamp: Utc::now(),
                pps_in: total_pps_in,
                pps_out: total_pps_out,
                bps_in: total_bps_in,
                bps_out: total_bps_out,
                udp_pps_in: udp_pps,
                udp_noports_pps: noports_pps,
                iface_rates,
            };

            // ── Threat detection ───────────────────────────────────────────
            self.detect_threats(&mut st, &sample).await;

            // Store sample
            if st.samples.len() >= MAX_SAMPLES {
                st.samples.pop_front();
            }
            st.samples.push_back(sample);

            // ── Expire old blacklist entries ────────────────────────────────
            let now = Utc::now();
            st.blacklist.retain(|_, v| v.expires_at > now);
        }

        st.prev = Some(raw);
    }

    async fn detect_threats(&self, st: &mut MonitorState, sample: &ComputedSample) {
        let now = Utc::now();

        // 1. Global PPS flood
        if sample.pps_in > THRESH_PPS_CRITICAL {
            push_event(&mut st.events, ThreatEvent {
                timestamp: now,
                event_type: "pps_flood".into(),
                severity: "critical".into(),
                description: format!("Inbound PPS at {:.0} exceeds critical threshold ({:.0})", sample.pps_in, THRESH_PPS_CRITICAL),
                source_ip: None,
                auto_mitigated: st.nft_initialised,
                metrics: HashMap::from([("pps".into(), sample.pps_in)]),
            });
            if st.nft_initialised {
                st.mitigation_active = true;
            }
        } else if sample.pps_in > THRESH_PPS_HIGH {
            push_event(&mut st.events, ThreatEvent {
                timestamp: now,
                event_type: "pps_flood".into(),
                severity: "high".into(),
                description: format!("Inbound PPS at {:.0} exceeds high threshold ({:.0})", sample.pps_in, THRESH_PPS_HIGH),
                source_ip: None,
                auto_mitigated: false,
                metrics: HashMap::from([("pps".into(), sample.pps_in)]),
            });
        } else if sample.pps_in > THRESH_PPS_WARN {
            push_event(&mut st.events, ThreatEvent {
                timestamp: now,
                event_type: "pps_elevated".into(),
                severity: "medium".into(),
                description: format!("Inbound PPS at {:.0} exceeds warning threshold ({:.0})", sample.pps_in, THRESH_PPS_WARN),
                source_ip: None,
                auto_mitigated: false,
                metrics: HashMap::from([("pps".into(), sample.pps_in)]),
            });
        }

        // 2. PPS spike detection (sudden surge vs 30s avg)
        let avg_30 = if st.samples.len() >= 5 {
            let n = st.samples.len().min(30);
            st.samples.iter().rev().take(n).map(|s| s.pps_in).sum::<f64>() / n as f64
        } else { 0.0 };

        if avg_30 > 100.0 && sample.pps_in / avg_30 > THRESH_SPIKE_RATIO {
            push_event(&mut st.events, ThreatEvent {
                timestamp: now,
                event_type: "pps_spike".into(),
                severity: "high".into(),
                description: format!(
                    "PPS spike detected: {:.0} PPS (avg {:.0}, ratio {:.1}x)",
                    sample.pps_in, avg_30, sample.pps_in / avg_30
                ),
                source_ip: None,
                auto_mitigated: false,
                metrics: HashMap::from([
                    ("current_pps".into(), sample.pps_in),
                    ("avg_pps".into(), avg_30),
                    ("ratio".into(), sample.pps_in / avg_30),
                ]),
            });
        }

        // 3. UDP NoPorts — indicates reflection/scanning
        if sample.udp_noports_pps > THRESH_NOPORTS_PPS {
            push_event(&mut st.events, ThreatEvent {
                timestamp: now,
                event_type: "udp_noports".into(),
                severity: "medium".into(),
                description: format!(
                    "High UDP NoPorts rate: {:.0}/s (packets to closed ports — possible reflection/scan)",
                    sample.udp_noports_pps
                ),
                source_ip: None,
                auto_mitigated: false,
                metrics: HashMap::from([("noports_pps".into(), sample.udp_noports_pps)]),
            });
        }

        // 4. UDP flood (high UDP datagram rate)
        if sample.udp_pps_in > THRESH_PPS_HIGH {
            push_event(&mut st.events, ThreatEvent {
                timestamp: now,
                event_type: "udp_flood".into(),
                severity: if sample.udp_pps_in > THRESH_PPS_CRITICAL { "critical" } else { "high" }.into(),
                description: format!("UDP flood: {:.0} datagrams/s inbound", sample.udp_pps_in),
                source_ip: None,
                auto_mitigated: st.nft_initialised,
                metrics: HashMap::from([("udp_pps".into(), sample.udp_pps_in)]),
            });
        }
    }
}

// ---------------------------------------------------------------------------
// /proc readers
// ---------------------------------------------------------------------------

async fn read_raw_counters() -> RawCounters {
    let now = std::time::Instant::now();
    let interfaces = read_proc_net_dev().await;
    let (udp_in, udp_noports, udp_errors) = read_proc_net_snmp_udp().await;

    RawCounters {
        timestamp: now,
        interfaces,
        udp_in_datagrams: udp_in,
        udp_no_ports: udp_noports,
        udp_in_errors: udp_errors,
    }
}

async fn read_proc_net_dev() -> Vec<IfaceCounters> {
    let content = match tokio::fs::read_to_string("/proc/net/dev").await {
        Ok(c) => c,
        Err(_) => return Vec::new(),
    };
    let mut result = Vec::new();
    for line in content.lines().skip(2) {
        let line = line.trim();
        if let Some((iface, rest)) = line.split_once(':') {
            let fields: Vec<&str> = rest.split_whitespace().collect();
            if fields.len() >= 10 {
                result.push(IfaceCounters {
                    name: iface.trim().to_string(),
                    rx_bytes: fields[0].parse().unwrap_or(0),
                    rx_packets: fields[1].parse().unwrap_or(0),
                    tx_bytes: fields[8].parse().unwrap_or(0),
                    tx_packets: fields[9].parse().unwrap_or(0),
                });
            }
        }
    }
    result
}

/// Read UDP stats from `/proc/net/snmp`.
/// Returns (InDatagrams, NoPorts, InErrors).
async fn read_proc_net_snmp_udp() -> (u64, u64, u64) {
    let content = match tokio::fs::read_to_string("/proc/net/snmp").await {
        Ok(c) => c,
        Err(_) => return (0, 0, 0),
    };

    let lines: Vec<&str> = content.lines().collect();
    // Find the "Udp:" header line, next line has values
    for i in 0..lines.len().saturating_sub(1) {
        if lines[i].starts_with("Udp:") && lines[i].contains("InDatagrams") {
            let vals: Vec<&str> = lines[i + 1].split_whitespace().collect();
            // Udp: InDatagrams NoPorts InErrors OutDatagrams ...
            if vals.len() >= 4 {
                let in_dg = vals[1].parse().unwrap_or(0);
                let noports = vals[2].parse().unwrap_or(0);
                let in_err = vals[3].parse().unwrap_or(0);
                return (in_dg, noports, in_err);
            }
        }
    }
    (0, 0, 0)
}

/// Read game port (5520-5600) UDP socket info from `/proc/net/udp`.
async fn read_game_port_info() -> Vec<GamePortInfo> {
    let content = match tokio::fs::read_to_string("/proc/net/udp").await {
        Ok(c) => c,
        Err(_) => return Vec::new(),
    };

    let mut ports: HashMap<u16, GamePortInfo> = HashMap::new();
    for line in content.lines().skip(1) {
        let fields: Vec<&str> = line.split_whitespace().collect();
        if fields.len() < 13 { continue; }

        // Parse local_address (hex IP:port)
        if let Some(port_hex) = fields[1].split(':').nth(1) {
            if let Ok(port) = u16::from_str_radix(port_hex, 16) {
                if port >= 5520 && port <= 5600 {
                    // rx_queue:tx_queue at field[4]
                    let queues: Vec<&str> = fields[4].split(':').collect();
                    let rx_q = queues.first()
                        .and_then(|h| u64::from_str_radix(h, 16).ok())
                        .unwrap_or(0);
                    let drops = fields.get(12)
                        .and_then(|d| d.parse::<u64>().ok())
                        .unwrap_or(0);

                    let entry = ports.entry(port).or_insert(GamePortInfo {
                        port,
                        rx_queue: 0,
                        drops: 0,
                        active_connections: 0,
                    });
                    entry.rx_queue += rx_q;
                    entry.drops += drops;
                    entry.active_connections += 1;
                }
            }
        }
    }

    // Also check /proc/net/tcp for TCP game ports
    if let Ok(tcp_content) = tokio::fs::read_to_string("/proc/net/tcp").await {
        for line in tcp_content.lines().skip(1) {
            let fields: Vec<&str> = line.split_whitespace().collect();
            if fields.len() < 4 { continue; }
            if let Some(port_hex) = fields[1].split(':').nth(1) {
                if let Ok(port) = u16::from_str_radix(port_hex, 16) {
                    if port >= 5520 && port <= 5600 {
                        let entry = ports.entry(port).or_insert(GamePortInfo {
                            port, rx_queue: 0, drops: 0, active_connections: 0,
                        });
                        entry.active_connections += 1;
                    }
                }
            }
        }
    }

    let mut result: Vec<GamePortInfo> = ports.into_values().collect();
    result.sort_by_key(|p| p.port);
    result
}

/// Read connection summary from /proc/net/tcp — total connections + SYN_RECV count.
async fn read_connection_summary() -> (usize, usize) {
    let content = match tokio::fs::read_to_string("/proc/net/tcp").await {
        Ok(c) => c,
        Err(_) => return (0, 0),
    };

    let mut total = 0usize;
    let mut syn_recv = 0usize;

    for line in content.lines().skip(1) {
        let fields: Vec<&str> = line.split_whitespace().collect();
        if fields.len() < 4 { continue; }
        if let Some(port_hex) = fields[1].split(':').nth(1) {
            if let Ok(port) = u16::from_str_radix(port_hex, 16) {
                if port >= 5520 && port <= 5600 {
                    total += 1;
                    if fields[3] == "03" { // SYN_RECV
                        syn_recv += 1;
                    }
                }
            }
        }
    }

    (total, syn_recv)
}

/// Read top talkers from conntrack (if available).
async fn read_top_talkers(blacklist: &HashMap<String, BlacklistEntry>) -> Vec<TopTalker> {
    // Try conntrack -L first
    let output = tokio::process::Command::new("conntrack")
        .args(["-L", "-p", "udp", "--dport", "5520"])
        .output()
        .await;

    let mut ip_counts: HashMap<String, usize> = HashMap::new();

    if let Ok(out) = output {
        if out.status.success() {
            let text = String::from_utf8_lossy(&out.stdout);
            for line in text.lines() {
                // Parse "src=1.2.3.4" from conntrack output
                for part in line.split_whitespace() {
                    if let Some(ip) = part.strip_prefix("src=") {
                        // Skip private/internal IPs
                        if !ip.starts_with("10.") && !ip.starts_with("172.") && !ip.starts_with("127.") {
                            *ip_counts.entry(ip.to_string()).or_insert(0) += 1;
                        }
                    }
                }
            }
        }
    }

    // Fall back to /proc/net/nf_conntrack if conntrack command not available
    if ip_counts.is_empty() {
        if let Ok(content) = tokio::fs::read_to_string("/proc/net/nf_conntrack").await {
            for line in content.lines() {
                if !line.contains("udp") { continue; }
                // Check if it involves game ports
                let has_game_port = line.contains("dport=5520") ||
                    line.contains("dport=5521") || line.contains("dport=5522") ||
                    line.contains("dport=5523") || line.contains("dport=5524") ||
                    line.contains("dport=5525");
                if !has_game_port { continue; }

                for part in line.split_whitespace() {
                    if let Some(ip) = part.strip_prefix("src=") {
                        if !ip.starts_with("10.") && !ip.starts_with("172.") && !ip.starts_with("127.") {
                            *ip_counts.entry(ip.to_string()).or_insert(0) += 1;
                        }
                    }
                }
            }
        }
    }

    let mut talkers: Vec<TopTalker> = ip_counts.into_iter()
        .map(|(ip, count)| {
            let is_bl = blacklist.contains_key(&ip);
            let severity = if count > 100 { "critical" }
                else if count > 50 { "high" }
                else if count > 20 { "medium" }
                else if count > 10 { "low" }
                else { "normal" };
            TopTalker {
                ip,
                pps: count as f64, // conntrack entries, not actual PPS
                connections: count,
                severity: severity.into(),
                blacklisted: is_bl,
            }
        })
        .collect();

    talkers.sort_by(|a, b| b.connections.cmp(&a.connections));
    talkers.truncate(50);
    talkers
}

// ---------------------------------------------------------------------------
// nftables mitigation
// ---------------------------------------------------------------------------

async fn check_nft_available() -> bool {
    tokio::process::Command::new("nft")
        .arg("--version")
        .output()
        .await
        .map(|o| o.status.success())
        .unwrap_or(false)
}

async fn init_nft_rules() -> anyhow::Result<()> {
    // Create table
    run_nft("add table inet taledaemon").await?;

    // Create blacklist set with timeout support
    run_nft("add set inet taledaemon blacklist { type ipv4_addr; flags timeout; }").await?;

    // Create input chain
    run_nft("add chain inet taledaemon input { type filter hook input priority -10 \\; policy accept \\; }").await?;

    // Drop blacklisted IPs
    run_nft("add rule inet taledaemon input ip saddr @blacklist counter drop").await?;

    // Rate limit per-IP on game ports: drop if >5000 pps per source
    run_nft(
        "add rule inet taledaemon input udp dport 5520-5600 meter per_ip_udp { ip saddr limit rate over 5000/second } counter drop"
    ).await?;

    // Global rate limit on game ports: drop if >50000 pps total
    run_nft(
        "add rule inet taledaemon input udp dport 5520-5600 limit rate over 50000/second counter drop"
    ).await?;

    // Counter rule for monitoring (must be after rate limits)
    run_nft("add rule inet taledaemon input udp dport 5520-5600 counter").await?;

    info!("nftables rules initialised for game port protection");
    Ok(())
}

async fn run_nft(rule: &str) -> anyhow::Result<()> {
    let parts: Vec<&str> = rule.split_whitespace().collect();
    let output = tokio::process::Command::new("nft")
        .args(&parts)
        .output()
        .await?;

    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr);
        // Ignore "already exists" errors
        if !stderr.contains("File exists") {
            anyhow::bail!("nft command failed: {}", stderr.trim());
        }
    }
    Ok(())
}

/// Add an IP to the nftables blacklist with a timeout.
#[allow(dead_code)]
async fn blacklist_ip(ip: &str, duration_secs: u64) -> anyhow::Result<()> {
    let cmd = format!("add element inet taledaemon blacklist {{ {} timeout {}s }}", ip, duration_secs);
    run_nft(&cmd).await?;
    info!(ip, duration_secs, "IP blacklisted via nftables");
    Ok(())
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

fn push_event(events: &mut VecDeque<ThreatEvent>, event: ThreatEvent) {
    // Dedup: skip if a same-type event was already recorded in the last 5 seconds
    if let Some(last) = events.back() {
        if last.event_type == event.event_type {
            let diff = event.timestamp.signed_duration_since(last.timestamp);
            if diff.num_seconds() < 5 {
                return;
            }
        }
    }

    debug!(event_type = %event.event_type, severity = %event.severity, "Threat event recorded");

    if events.len() >= MAX_EVENTS {
        events.pop_front();
    }
    events.push_back(event);
}

struct Avg5s {
    pps_in: f64,
    bps_in: f64,
}

struct Avg30s {
    pps_in: f64,
    bps_in: f64,
}

impl Avg5s {
    fn merge_30(&self, a30: &Avg30s) -> AverageRates {
        AverageRates {
            pps_in_5s: self.pps_in,
            pps_in_30s: a30.pps_in,
            bps_in_5s: self.bps_in,
            bps_in_30s: a30.bps_in,
        }
    }
}

fn avg_window(samples: &VecDeque<ComputedSample>, n: usize) -> Avg5s {
    let count = samples.len().min(n);
    if count == 0 {
        return Avg5s { pps_in: 0.0, bps_in: 0.0 };
    }
    let (sum_pps, sum_bps) = samples.iter().rev().take(count)
        .fold((0.0, 0.0), |(p, b), s| (p + s.pps_in, b + s.bps_in));
    Avg5s {
        pps_in: sum_pps / count as f64,
        bps_in: sum_bps / count as f64,
    }
}

fn compute_threat_level(st: &MonitorState, current: &CurrentRates, syn_count: usize) -> String {
    if current.pps_in > THRESH_PPS_CRITICAL || current.udp_pps_in > THRESH_PPS_CRITICAL {
        return "critical".into();
    }
    if current.pps_in > THRESH_PPS_HIGH || syn_count > THRESH_SYN_RECV * 2 {
        return "high".into();
    }
    if current.pps_in > THRESH_PPS_WARN || syn_count > THRESH_SYN_RECV
        || current.udp_noports_pps > THRESH_NOPORTS_PPS
    {
        return "medium".into();
    }

    // Check spike
    if st.samples.len() >= 5 {
        let avg = st.samples.iter().rev().take(30).map(|s| s.pps_in).sum::<f64>()
            / st.samples.len().min(30) as f64;
        if avg > 100.0 && current.pps_in / avg > THRESH_SPIKE_RATIO {
            return "high".into();
        }
    }

    "clear".into()
}
