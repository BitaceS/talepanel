use tauri::State;
use serde::{Deserialize, Serialize};

use crate::api::{ApiClient, LoginRequest, LoginResponse, Server, Node};
use crate::state::AppState;

#[derive(Debug, Serialize, Deserialize)]
pub struct ConnectRequest {
    pub api_url: String,
    pub email: String,
    pub password: String,
}

#[derive(Debug, Serialize)]
pub struct ConnectResult {
    pub success: bool,
    pub username: Option<String>,
    pub error: Option<String>,
}

#[tauri::command]
pub async fn connect_to_panel(
    state: State<'_, AppState>,
    req: ConnectRequest,
) -> Result<ConnectResult, String> {
    let temp_client = ApiClient::new(&req.api_url, "");

    let login_body = LoginRequest {
        email: req.email,
        password: req.password,
    };

    match temp_client
        .post::<LoginRequest, LoginResponse>("/auth/login", &login_body)
        .await
    {
        Ok(resp) => {
            if let Some(token) = resp.access_token {
                let mut conn = state.connection.lock().unwrap();
                conn.api_url = req.api_url;
                conn.access_token = token;
                conn.connected = true;

                Ok(ConnectResult {
                    success: true,
                    username: resp.user.map(|u| u.username),
                    error: None,
                })
            } else {
                Ok(ConnectResult {
                    success: false,
                    username: None,
                    error: Some("Login failed: no token returned".to_string()),
                })
            }
        }
        Err(e) => Ok(ConnectResult {
            success: false,
            username: None,
            error: Some(e.to_string()),
        }),
    }
}

#[tauri::command]
pub async fn disconnect(state: State<'_, AppState>) -> Result<(), String> {
    let mut conn = state.connection.lock().unwrap();
    conn.api_url = String::new();
    conn.access_token = String::new();
    conn.connected = false;
    Ok(())
}

#[tauri::command]
pub async fn get_connection_status(state: State<'_, AppState>) -> Result<bool, String> {
    let conn = state.connection.lock().unwrap();
    Ok(conn.connected)
}

fn get_client(state: &State<'_, AppState>) -> Result<ApiClient, String> {
    let conn = state.connection.lock().unwrap();
    if !conn.connected {
        return Err("Not connected to a TalePanel instance".to_string());
    }
    Ok(ApiClient::new(&conn.api_url, &conn.access_token))
}

#[tauri::command]
pub async fn get_servers(state: State<'_, AppState>) -> Result<Vec<Server>, String> {
    let client = get_client(&state)?;
    client.get::<Vec<Server>>("/servers").await.map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn get_server(state: State<'_, AppState>, id: String) -> Result<Server, String> {
    let client = get_client(&state)?;
    client.get::<Server>(&format!("/servers/{}", id)).await.map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn start_server(state: State<'_, AppState>, id: String) -> Result<(), String> {
    let client = get_client(&state)?;
    client
        .post::<serde_json::Value, serde_json::Value>(&format!("/servers/{}/start", id), &serde_json::json!({}))
        .await
        .map(|_| ())
        .map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn stop_server(state: State<'_, AppState>, id: String) -> Result<(), String> {
    let client = get_client(&state)?;
    client
        .post::<serde_json::Value, serde_json::Value>(&format!("/servers/{}/stop", id), &serde_json::json!({}))
        .await
        .map(|_| ())
        .map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn restart_server(state: State<'_, AppState>, id: String) -> Result<(), String> {
    let client = get_client(&state)?;
    client
        .post::<serde_json::Value, serde_json::Value>(&format!("/servers/{}/restart", id), &serde_json::json!({}))
        .await
        .map(|_| ())
        .map_err(|e| e.to_string())
}

#[tauri::command]
pub async fn get_nodes(state: State<'_, AppState>) -> Result<Vec<Node>, String> {
    let client = get_client(&state)?;
    client.get::<Vec<Node>>("/nodes").await.map_err(|e| e.to_string())
}
