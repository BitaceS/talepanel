pub mod hytale;
pub mod manager;

// Keep mock available for unit tests only
#[cfg(test)]
pub mod mock;

pub use hytale::{HytaleServerProcess, ServerConfig, ServerMetrics};
pub use manager::ProcessManager;

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

// ---------------------------------------------------------------------------
// Shared process types
// ---------------------------------------------------------------------------

/// Lifecycle state of a managed Hytale server process.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum ServerStatus {
    /// Process is not running and has no associated handle.
    Stopped,
    /// Process is launching; not yet accepting player connections.
    Starting,
    /// Process is fully initialised and accepting player connections.
    Running,
    /// A graceful shutdown has been requested; process is winding down.
    Stopping,
    /// Process exited unexpectedly (non-zero exit code or signal).
    Crashed,
}

impl std::fmt::Display for ServerStatus {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let s = match self {
            ServerStatus::Stopped => "stopped",
            ServerStatus::Starting => "starting",
            ServerStatus::Running => "running",
            ServerStatus::Stopping => "stopping",
            ServerStatus::Crashed => "crashed",
        };
        write!(f, "{s}")
    }
}

/// A single line of output emitted by a server process (stdout or stderr).
///
/// This is the internal representation; see `crate::api_client::LogLine` for
/// the wire format sent to the TalePanel API.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LogLine {
    pub timestamp: DateTime<Utc>,
    /// Severity: "INFO" | "WARN" | "ERROR".
    pub level: String,
    pub message: String,
}

impl LogLine {
    pub fn info(message: impl Into<String>) -> Self {
        Self {
            timestamp: Utc::now(),
            level: "INFO".to_string(),
            message: message.into(),
        }
    }

    pub fn warn(message: impl Into<String>) -> Self {
        Self {
            timestamp: Utc::now(),
            level: "WARN".to_string(),
            message: message.into(),
        }
    }

    pub fn error(message: impl Into<String>) -> Self {
        Self {
            timestamp: Utc::now(),
            level: "ERROR".to_string(),
            message: message.into(),
        }
    }
}
