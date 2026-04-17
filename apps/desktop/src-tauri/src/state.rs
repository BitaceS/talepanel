use std::sync::Mutex;

#[derive(Default)]
pub struct ConnectionConfig {
    pub api_url: String,
    pub access_token: String,
    pub connected: bool,
}

pub struct AppState {
    pub connection: Mutex<ConnectionConfig>,
}

impl AppState {
    pub fn new() -> Self {
        Self {
            connection: Mutex::new(ConnectionConfig::default()),
        }
    }
}
