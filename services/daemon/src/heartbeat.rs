use std::collections::HashMap;
use std::sync::Arc;
use tokio::sync::broadcast;
use tracing::{info, instrument, warn};

use crate::api_client::{ApiClient, HeartbeatMetrics, ServerSample};
use crate::cert_renewal::NodePki;
use crate::config::Config;
use crate::disk_used_mb;
use crate::process::hytale::ServerMetrics;
use crate::process::ProcessManager;

/// Run the heartbeat loop until a shutdown signal is received.
///
/// Every `config.resources.heartbeat_interval_s` seconds this function:
///   1. Collects real host-level resource metrics via `sysinfo`.
///   2. Builds a `HeartbeatMetrics` snapshot.
///   3. Calls `api_client.heartbeat("online", metrics)`.
///
/// `pki` is `Some` only when the listener actually serves TLS with the node
/// cert. After each heartbeat it renews the certificate or adopts a new panel
/// CA bundle when due (see [`NodePki::on_heartbeat`]).
///
/// Heartbeat failures are logged at WARN level but do not stop the loop.
#[instrument(skip_all)]
pub async fn run_heartbeat_loop(
    api_client: Arc<ApiClient>,
    process_manager: Arc<ProcessManager>,
    config: Arc<Config>,
    pki: Option<Arc<NodePki>>,
    mut shutdown: broadcast::Receiver<()>,
) {
    let interval_duration = tokio::time::Duration::from_secs(config.resources.heartbeat_interval_s);
    let mut interval = tokio::time::interval(interval_duration);
    // Skip missed ticks rather than bursting when the system is slow.
    interval.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);

    // Fire an immediate heartbeat on startup so the API knows we are online
    // as soon as possible, rather than waiting for the first interval.
    send_heartbeat(
        &api_client,
        &process_manager,
        &config,
        pki.as_deref(),
        "online",
    )
    .await;

    info!(
        interval_s = config.resources.heartbeat_interval_s,
        "Heartbeat loop started"
    );

    loop {
        tokio::select! {
            _ = interval.tick() => {
                send_heartbeat(&api_client, &process_manager, &config, pki.as_deref(), "online").await;
            }

            _ = shutdown.recv() => {
                info!("Heartbeat loop received shutdown signal; exiting");
                break;
            }
        }
    }
}

/// Collect metrics, build a heartbeat payload, and send it to the API.
///
/// `node_status` should be `"online"`, `"degraded"`, or `"offline"`.
async fn send_heartbeat(
    api_client: &Arc<ApiClient>,
    process_manager: &Arc<ProcessManager>,
    config: &Arc<Config>,
    pki: Option<&NodePki>,
    node_status: &str,
) {
    let all_metrics = process_manager.collect_all_metrics().await;
    let server_count = all_metrics.len();

    // Sum CPU across all running server processes for the "servers are busy" signal.
    let server_cpu_sum: f32 = all_metrics.values().map(|m| m.cpu_percent).sum();
    let avg_server_cpu = if server_count > 0 {
        server_cpu_sum / server_count as f32
    } else {
        0.0
    };

    // Real host-level metrics from sysinfo.
    // CPU is the usage since the previous heartbeat (shared sampler), not the
    // average since boot that a fresh System reports.
    let (host_cpu, host_ram_used_mb) = tokio::task::spawn_blocking(crate::sysmetrics::host_usage)
        .await
        .unwrap_or((0.0, 0));

    // Report the higher of host CPU or average server CPU so the panel never
    // shows 0% when servers are clearly working (sysinfo lags by one sample).
    let reported_cpu = host_cpu.max(avg_server_cpu);

    let servers = server_samples(&all_metrics);

    let metrics = HeartbeatMetrics {
        cpu_percent: reported_cpu,
        ram_used_mb: host_ram_used_mb,
        disk_used_mb: disk_used_mb(&config.daemon.data_root),
        server_count,
        timestamp: chrono::Utc::now(),
        mtls: pki.map(NodePki::advert),
        servers,
    };

    match api_client.heartbeat(node_status, metrics).await {
        Ok(hint) => {
            if let Some(pki) = pki {
                pki.on_heartbeat(api_client, hint.as_ref()).await;
            }
        }
        Err(err) => warn!(%err, "Heartbeat failed"),
    }
}

/// Per-server samples for the panel's performance graphs. A process without a
/// live PID reports zero CPU and RAM; those are left out rather than drawn as
/// a drop to zero.
fn server_samples(all: &HashMap<String, ServerMetrics>) -> Vec<ServerSample> {
    let mut out: Vec<ServerSample> = all
        .iter()
        .filter(|(_, m)| m.ram_mb > 0 || m.cpu_percent > 0.0)
        .map(|(id, m)| ServerSample {
            server_id: id.clone(),
            cpu_percent: m.cpu_percent,
            ram_mb: m.ram_mb,
        })
        .collect();
    out.sort_by(|a, b| a.server_id.cmp(&b.server_id));
    out
}

#[cfg(test)]
mod sample_tests {
    use super::*;

    #[test]
    fn idle_processes_are_left_out() {
        let mut all = HashMap::new();
        all.insert(
            "b".to_string(),
            ServerMetrics {
                cpu_percent: 5.0,
                ram_mb: 900,
                uptime_s: 60,
            },
        );
        all.insert(
            "a".to_string(),
            ServerMetrics {
                cpu_percent: 0.0,
                ram_mb: 0,
                uptime_s: 0,
            },
        );
        let s = server_samples(&all);
        assert_eq!(
            s,
            vec![ServerSample {
                server_id: "b".into(),
                cpu_percent: 5.0,
                ram_mb: 900
            }]
        );
    }
}

/// sysinfo 0.30 reports memory in bytes (older releases used KiB).
pub(crate) fn bytes_to_mb(bytes: u64) -> u64 {
    bytes / (1024 * 1024)
}

#[cfg(test)]
mod mem_unit_tests {
    use super::bytes_to_mb;

    #[test]
    fn converts_bytes_to_mebibytes() {
        assert_eq!(bytes_to_mb(0), 0);
        assert_eq!(bytes_to_mb(8 * 1024 * 1024 * 1024), 8192);
        assert_eq!(bytes_to_mb(1024 * 1024 - 1), 0);
    }

    #[test]
    fn sysinfo_total_memory_is_plausible_in_mb() {
        let mut sys = sysinfo::System::new();
        sys.refresh_memory();
        let mb = bytes_to_mb(sys.total_memory());
        // Any real host has between 64 MiB and 64 TiB of RAM.
        assert!((64..64 * 1024 * 1024).contains(&mb), "total RAM {mb} MB");
    }
}
