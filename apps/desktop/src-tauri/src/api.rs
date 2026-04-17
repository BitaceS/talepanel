use anyhow::{anyhow, Result};
use serde::{Deserialize, Serialize};

#[derive(Clone)]
pub struct ApiClient {
    pub base_url: String,
    pub token: String,
    client: reqwest::Client,
}

impl ApiClient {
    pub fn new(base_url: &str, token: &str) -> Self {
        let client = reqwest::Client::builder()
            .timeout(std::time::Duration::from_secs(30))
            .user_agent("TalePanelDesktop/0.1.0")
            .build()
            .expect("failed to build HTTP client");

        Self {
            base_url: base_url.trim_end_matches('/').to_string(),
            token: token.to_string(),
            client,
        }
    }

    pub async fn get<T: for<'de> Deserialize<'de>>(&self, path: &str) -> Result<T> {
        let url = format!("{}/api/v1{}", self.base_url, path);
        let resp = self
            .client
            .get(&url)
            .bearer_auth(&self.token)
            .send()
            .await?;

        if !resp.status().is_success() {
            return Err(anyhow!("API error {}: {}", resp.status(), path));
        }

        Ok(resp.json::<T>().await?)
    }

    pub async fn post<B: Serialize, T: for<'de> Deserialize<'de>>(
        &self,
        path: &str,
        body: &B,
    ) -> Result<T> {
        let url = format!("{}/api/v1{}", self.base_url, path);
        let resp = self
            .client
            .post(&url)
            .bearer_auth(&self.token)
            .json(body)
            .send()
            .await?;

        if !resp.status().is_success() {
            return Err(anyhow!("API error {}: {}", resp.status(), path));
        }

        Ok(resp.json::<T>().await?)
    }
}

// ─── API Response types ───────────────────────────────────────────────────────

#[derive(Debug, Serialize, Deserialize, Clone)]
pub struct User {
    pub id: String,
    pub email: String,
    pub username: String,
    pub role: String,
}

#[derive(Debug, Serialize, Deserialize, Clone)]
pub struct Server {
    pub id: String,
    pub name: String,
    pub status: String,
    pub hytale_version: String,
    pub port: u16,
    pub auto_restart: bool,
    pub ram_limit_mb: Option<i64>,
    pub cpu_limit: Option<i64>,
}

#[derive(Debug, Serialize, Deserialize, Clone)]
pub struct Node {
    pub id: String,
    pub name: String,
    pub fqdn: String,
    pub status: String,
    pub total_cpu: i32,
    pub total_ram_mb: i64,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct LoginRequest {
    pub email: String,
    pub password: String,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct LoginResponse {
    pub access_token: Option<String>,
    pub user: Option<User>,
    pub requires_totp: Option<bool>,
}
