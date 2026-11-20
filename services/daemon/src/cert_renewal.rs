//! Node certificate renewal while running.
//!
//! The daemon renews its certificate when it enters the renewal window
//! (30 days before expiry) or when the panel says it comes from a CA that is
//! no longer the active one (CA rotation). The panel's trust bundle from the
//! heartbeat answer replaces the local one when it only drops CAs or adds
//! CAs endorsed by one this node already trusts. Every change is applied to
//! the TLS listener first and written to disk afterwards, so a set the
//! listener cannot use is never persisted or advertised.

use std::path::PathBuf;
use std::sync::{Arc, RwLock};
use std::time::{Duration, Instant};

use anyhow::{Context, Result};
use tracing::{info, warn};

use crate::api_client::{ApiClient, MtlsAdvert, PkiHint};
use crate::http_server::TlsHandle;
use crate::pki::{self, LoadedPki};

/// Wait this long after a failed renewal before trying again.
const RETRY_AFTER: Duration = Duration::from_secs(10 * 60);

pub struct NodePki {
    dir: PathBuf,
    node_id: String,
    tls: Arc<TlsHandle>,
    current: RwLock<LoadedPki>,
    /// Serialises renewals and trust updates.
    busy: tokio::sync::Mutex<()>,
    /// No renewal attempt before this instant (backoff after a failure).
    not_before: std::sync::Mutex<Option<Instant>>,
}

impl NodePki {
    pub fn new(dir: PathBuf, node_id: String, tls: Arc<TlsHandle>, pki: LoadedPki) -> Self {
        Self {
            dir,
            node_id,
            tls,
            current: RwLock::new(pki),
            busy: tokio::sync::Mutex::new(()),
            not_before: std::sync::Mutex::new(None),
        }
    }

    fn get(&self) -> LoadedPki {
        self.current
            .read()
            .unwrap_or_else(|e| e.into_inner())
            .clone()
    }

    /// What the heartbeat advertises: the certificate the listener serves.
    pub fn advert(&self) -> MtlsAdvert {
        let p = self.get();
        MtlsAdvert {
            serial: p.serial,
            not_after: p.not_after,
        }
    }

    /// Put `next` into service: listener first, then disk, then memory.
    fn install(&self, next: LoadedPki) -> Result<()> {
        self.tls
            .replace(&next)
            .context("TLS listener rejected the new certificate set")?;
        if let Err(e) = pki::write_pki(&self.dir, &next.ca_pem, &next.cert_pem, &next.key_pem) {
            // Served already; the files catch up on the next change. After a
            // restart the old (still trusted) set is loaded and renewed again.
            warn!(error = %e, "Writing renewed node PKI failed");
        }
        *self.current.write().unwrap_or_else(|e| e.into_inner()) = next;
        Ok(())
    }

    fn backoff_active(&self) -> bool {
        let guard = self.not_before.lock().unwrap_or_else(|e| e.into_inner());
        guard.is_some_and(|t| Instant::now() < t)
    }

    fn set_backoff(&self, failed: bool) {
        *self.not_before.lock().unwrap_or_else(|e| e.into_inner()) =
            failed.then(|| Instant::now() + RETRY_AFTER);
    }

    /// Act on a heartbeat: renew if the panel asks for it or the certificate
    /// is close to expiry, else adopt the panel's trust bundle if it changed.
    pub async fn on_heartbeat(&self, api: &ApiClient, hint: Option<&PkiHint>) {
        let _guard = self.busy.lock().await;
        let current = self.get();
        let panel_asks = hint.is_some_and(|h| h.renew);
        if panel_asks || pki::needs_renewal(&current) {
            if self.backoff_active() {
                return;
            }
            let reason = if panel_asks {
                "panel CA rotation"
            } else {
                "certificate close to expiry"
            };
            match self.renew(api, &current).await {
                Ok(()) => {
                    self.set_backoff(false);
                    let p = self.get();
                    info!(reason, serial = %p.serial, not_after = %p.not_after, "Node certificate renewed");
                }
                Err(e) => {
                    self.set_backoff(true);
                    warn!(reason, error = %format!("{e:#}"), retry_in_s = RETRY_AFTER.as_secs(), "Node certificate renewal failed");
                }
            }
            return;
        }
        let Some(h) = hint else { return };
        match pki::with_trust(&current, &h.ca_bundle_pem, &h.endorsements_pem) {
            Ok(None) => {}
            Ok(Some(next)) => match self.install(next) {
                Ok(()) => info!("Updated trusted panel CAs"),
                Err(e) => warn!(error = %format!("{e:#}"), "Applying panel CA bundle failed"),
            },
            Err(e) => warn!(error = %format!("{e:#}"), "Ignoring panel CA bundle"),
        }
    }

    async fn renew(&self, api: &ApiClient, current: &LoadedPki) -> Result<()> {
        let next = pki::renew(api, &self.node_id, current).await?;
        self.install(next)
    }
}
