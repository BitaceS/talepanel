mod api;
mod commands;
mod state;

use state::AppState;
use tauri::Manager;

pub fn run() {
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::from_default_env()
                .add_directive("talepanel_desktop=debug".parse().unwrap()),
        )
        .init();

    tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_fs::init())
        .plugin(tauri_plugin_http::init())
        .plugin(tauri_plugin_notification::init())
        .plugin(tauri_plugin_store::Builder::new().build())
        .manage(AppState::new())
        .invoke_handler(tauri::generate_handler![
            commands::connect_to_panel,
            commands::disconnect,
            commands::get_servers,
            commands::get_server,
            commands::start_server,
            commands::stop_server,
            commands::restart_server,
            commands::get_nodes,
            commands::get_connection_status,
        ])
        .setup(|app| {
            let _window = app.get_webview_window("main").unwrap();

            #[cfg(debug_assertions)]
            _window.open_devtools();

            tracing::info!("TalePanel Desktop started");
            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("error while running TalePanel Desktop");
}
