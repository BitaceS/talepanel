mod api_client;
mod command_poller;
mod config;
mod downloader;
mod heartbeat;
mod http_server;
mod net_monitor;
mod plugin_scanner;
mod process;

use std::net::SocketAddr;
use std::sync::Arc;

use anyhow::{Context, Result};
use tokio::sync::broadcast;
use tracing::{error, info, warn};
use tracing_subscriber::{fmt, EnvFilter};

use api_client::{ApiClient, HeartbeatMetrics, NodeRegistration};
use config::Config;
use http_server::AppState;
use process::ProcessManager;

// ---------------------------------------------------------------------------
// Entry point
// ---------------------------------------------------------------------------

#[tokio::main]
async fn main() -> Result<()> {
    // ── 1. Load config ──────────────────────────────────────────────────────
    //
    // Priority: /etc/taledaemon/config.toml → ./config.toml → env vars.
    // We bootstrap a minimal subscriber before loading config so that any
    // errors during loading are visible.
    init_bootstrap_tracing();

    let config = Config::load().context("Failed to load configuration")?;
    let config = Arc::new(config);

    // ── 2. Reinitialise tracing with the configured log level & format ──────
    init_tracing(&config);

    info!(
        node_id = %config.daemon.node_id,
        api_url = %config.daemon.api_url,
        env     = %config.daemon.env,
        version = env!("CARGO_PKG_VERSION"),
        "TaleDaemon starting"
    );

    // ── 3. Construct shared services ────────────────────────────────────────
    let api_client = Arc::new(ApiClient::new(
        &config.daemon.api_url,
        &config.daemon.node_token,
        &config.daemon.node_id,
    ));

    let process_manager = Arc::new(ProcessManager::new(
        Arc::clone(&api_client),
        Arc::clone(&config),
    ));

    // ── 4. Register this node with the TalePanel API ────────────────────────
    let node_info = build_node_registration(&config);
    api_client
        .register(node_info)
        .await
        .context("Node registration failed; check api_url and node_token")?;

    info!("Node registered with TalePanel API");

    // ── 5. Set up the graceful-shutdown broadcast channel ───────────────────
    //
    // The channel capacity of 8 ensures all background tasks receive the
    // signal even if they aren't polling immediately.
    let (shutdown_tx, _) = broadcast::channel::<()>(8);

    // ── 6. Spawn background tasks ────────────────────────────────────────────

    // 6a. Heartbeat task
    let heartbeat_handle = {
        let api = Arc::clone(&api_client);
        let pm = Arc::clone(&process_manager);
        let cfg = Arc::clone(&config);
        let rx = shutdown_tx.subscribe();
        tokio::spawn(async move {
            heartbeat::run_heartbeat_loop(api, pm, cfg, rx).await;
        })
    };

    // 6b. Command-poller task
    let poller_handle = {
        let api = Arc::clone(&api_client);
        let pm = Arc::clone(&process_manager);
        let cfg = Arc::clone(&config);
        let rx = shutdown_tx.subscribe();
        tokio::spawn(async move {
            command_poller::run_command_poller(api, pm, cfg, rx).await;
        })
    };

    // 6c. Plugin scanner task (every 5 min)
    let scanner_handle = {
        let api = Arc::clone(&api_client);
        let cfg = Arc::clone(&config);
        let rx = shutdown_tx.subscribe();
        tokio::spawn(async move {
            plugin_scanner::run_plugin_scanner(api, cfg, rx).await;
        })
    };

    // 6d. Network monitor (background 1-second sampling loop)
    let net_mon = net_monitor::NetMonitor::new();
    let net_mon_handle = {
        let mon = net_mon.clone();
        tokio::spawn(async move { mon.run().await; })
    };

    // 6e. Local Axum HTTP server
    let listen_addr: SocketAddr = format!(
        "0.0.0.0:{}",
        config.daemon.listen_port
    )
    .parse()
    .context("Invalid listen address")?;

    let http_state = AppState {
        process_manager: Arc::clone(&process_manager),
        node_id: config.daemon.node_id.clone(),
        node_token: config.daemon.node_token.clone(),
        servers_base_dir: format!("{}/servers", config.daemon.data_root),
        net_monitor: net_mon,
    };

    let http_handle = tokio::spawn(async move {
        if let Err(err) = http_server::run_http_server(http_state, listen_addr).await {
            error!(%err, "Local HTTP server exited with error");
        }
    });

    // ── 7. Wait for a shutdown signal ────────────────────────────────────────
    wait_for_shutdown_signal().await;
    info!("Shutdown signal received; beginning graceful shutdown");

    // ── 8. Broadcast shutdown to background tasks ────────────────────────────
    //
    // Errors here just mean there are no subscribers, which is fine.
    let _ = shutdown_tx.send(());

    // ── 9. Stop all running server processes ────────────────────────────────
    process_manager.stop_all().await;

    // ── 10. Send a final "offline" heartbeat ────────────────────────────────
    let offline_metrics = HeartbeatMetrics {
        cpu_percent: 0.0,
        ram_used_mb: 0,
        disk_used_mb: 0,
        server_count: 0,
        timestamp: chrono::Utc::now(),
    };
    if let Err(err) = api_client.heartbeat("offline", offline_metrics).await {
        warn!(%err, "Failed to send offline heartbeat (non-fatal)");
    }

    // ── 11. Await background tasks (with a short deadline) ──────────────────
    let _ = tokio::time::timeout(
        tokio::time::Duration::from_secs(5),
        async {
            let _ = heartbeat_handle.await;
            let _ = poller_handle.await;
            let _ = scanner_handle.await;
        },
    )
    .await;

    // The HTTP server and network monitor tasks are not cancelable via the
    // broadcast channel, so we abort them directly.
    http_handle.abort();
    net_mon_handle.abort();

    info!("TaleDaemon shut down cleanly");
    Ok(())
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/// Initialise a minimal tracing subscriber used only until the config is loaded.
fn init_bootstrap_tracing() {
    let _ = tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::from_default_env())
        .try_init();
}

/// Reinitialise tracing with the log level and format specified in the config.
///
/// In production (`env = "production"`) logs are emitted as JSON objects so
/// that they can be ingested by log aggregators (Loki, Datadog, etc.).
/// In development they are emitted in a human-readable pretty format.
fn init_tracing(config: &Config) {
    // Construct the filter from the configured level, falling back to "info".
    let filter = EnvFilter::try_from_default_env()
        .unwrap_or_else(|_| EnvFilter::new(&config.daemon.log_level));

    if config.is_production() {
        let _ = tracing_subscriber::fmt()
            .json()
            .with_env_filter(filter)
            .try_init();
    } else {
        let _ = tracing_subscriber::fmt()
            .pretty()
            .with_env_filter(filter)
            .try_init();
    }
}

/// Build the `NodeRegistration` payload using live host information.
fn build_node_registration(config: &Config) -> NodeRegistration {
    let mut sys = sysinfo::System::new();
    sys.refresh_cpu_usage();
    sys.refresh_memory();

    let cpu_cores = sys.cpus().len().max(
        std::thread::available_parallelism()
            .map(|n| n.get())
            .unwrap_or(1),
    ) as u32;

    let total_ram_mb = sys.total_memory() / 1024; // sysinfo KB → MB

    let total_disk_mb = disk_total_mb(&config.daemon.data_root);

    NodeRegistration {
        node_id: config.daemon.node_id.clone(),
        cpu_cores,
        total_ram_mb,
        total_disk_mb,
        version: env!("CARGO_PKG_VERSION").to_string(),
    }
}

/// Returns the total disk space (in MB) on the partition that contains `path`.
fn disk_total_mb(path: &str) -> u64 {
    let target = std::path::Path::new(path);
    let disks = sysinfo::Disks::new_with_refreshed_list();

    // Pick the disk whose mount point is the longest prefix of `path`.
    disks
        .list()
        .iter()
        .filter(|d| target.starts_with(d.mount_point()))
        .max_by_key(|d| d.mount_point().to_string_lossy().len())
        .map(|d| d.total_space() / (1024 * 1024))
        .unwrap_or(0)
}

/// Returns disk space currently in use (in MB) on the partition containing `path`.
pub fn disk_used_mb(path: &str) -> u64 {
    let target = std::path::Path::new(path);
    let disks = sysinfo::Disks::new_with_refreshed_list();

    disks
        .list()
        .iter()
        .filter(|d| target.starts_with(d.mount_point()))
        .max_by_key(|d| d.mount_point().to_string_lossy().len())
        .map(|d| {
            let total = d.total_space() / (1024 * 1024);
            let avail = d.available_space() / (1024 * 1024);
            total.saturating_sub(avail)
        })
        .unwrap_or(0)
}

/// Wait for SIGINT (Ctrl-C) or SIGTERM (on Unix) using Tokio's signal API.
async fn wait_for_shutdown_signal() {
    #[cfg(unix)]
    {
        use tokio::signal::unix::{signal, SignalKind};

        let mut sigint = signal(SignalKind::interrupt()).expect("Failed to register SIGINT handler");
        let mut sigterm =
            signal(SignalKind::terminate()).expect("Failed to register SIGTERM handler");

        tokio::select! {
            _ = sigint.recv()  => info!("Received SIGINT"),
            _ = sigterm.recv() => info!("Received SIGTERM"),
        }
    }

    #[cfg(not(unix))]
    {
        // On Windows / other platforms fall back to Ctrl-C only.
        tokio::signal::ctrl_c()
            .await
            .expect("Failed to listen for Ctrl-C");
        info!("Received Ctrl-C");
    }
}
