use std::sync::Arc;
use tokio::sync::broadcast;
use tracing::{info, instrument, warn};

use crate::api_client::{ApiClient, HeartbeatMetrics, MtlsAdvert};
use crate::config::Config;
use crate::disk_used_mb;
use crate::process::ProcessManager;

/// Run the heartbeat loop until a shutdown signal is received.
///
/// Every `config.resources.heartbeat_interval_s` seconds this function:
///   1. Collects real host-level resource metrics via `sysinfo`.
///   2. Builds a `HeartbeatMetrics` snapshot.
///   3. Calls `api_client.heartbeat("online", metrics)`.
///
/// `mtls` is `Some` only when the listener actually serves TLS with the node cert.
///
/// Heartbeat failures are logged at WARN level but do not stop the loop.
#[instrument(skip_all)]
pub async fn run_heartbeat_loop(
    api_client: Arc<ApiClient>,
    process_manager: Arc<ProcessManager>,
    config: Arc<Config>,
    mtls: Option<MtlsAdvert>,
    mut shutdown: broadcast::Receiver<()>,
) {
    let interval_duration =
        tokio::time::Duration::from_secs(config.resources.heartbeat_interval_s);
    let mut interval = tokio::time::interval(interval_duration);
    // Skip missed ticks rather than bursting when the system is slow.
    interval.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);

    // Fire an immediate heartbeat on startup so the API knows we are online
    // as soon as possible, rather than waiting for the first interval.
    send_heartbeat(&api_client, &process_manager, &config, mtls.as_ref(), "online").await;

    info!(
        interval_s = config.resources.heartbeat_interval_s,
        "Heartbeat loop started"
    );

    loop {
        tokio::select! {
            _ = interval.tick() => {
                send_heartbeat(&api_client, &process_manager, &config, mtls.as_ref(), "online").await;
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
    mtls: Option<&MtlsAdvert>,
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
    let mut sys = sysinfo::System::new();
    sys.refresh_cpu_usage();
    sys.refresh_memory();
    let host_cpu = sys.global_cpu_info().cpu_usage();
    let host_ram_used_mb = bytes_to_mb(sys.used_memory());

    // Report the higher of host CPU or average server CPU so the panel never
    // shows 0% when servers are clearly working (sysinfo lags by one sample).
    let reported_cpu = host_cpu.max(avg_server_cpu);

    let metrics = HeartbeatMetrics {
        cpu_percent: reported_cpu,
        ram_used_mb: host_ram_used_mb,
        disk_used_mb: disk_used_mb(&config.daemon.data_root),
        server_count,
        timestamp: chrono::Utc::now(),
        mtls: mtls.cloned(),
    };

    if let Err(err) = api_client.heartbeat(node_status, metrics).await {
        warn!(%err, "Heartbeat failed");
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
