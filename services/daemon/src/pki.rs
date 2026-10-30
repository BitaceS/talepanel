//! Node PKI: generates this daemon's own ECDSA P-256 key + CSR, fetches a
//! panel-signed certificate and persists/loads the PEMs on disk.

use std::fs;
use std::io::Write;
use std::os::unix::fs::{DirBuilderExt, OpenOptionsExt, PermissionsExt};
use std::path::Path;

use anyhow::{bail, Context, Result};
use chrono::{DateTime, TimeZone, Utc};
use rcgen::{CertificateParams, DistinguishedName, DnType, KeyPair};
use tracing::{info, warn};

use crate::api_client::ApiClient;

const CA_FILE: &str = "ca.pem";
const CERT_FILE: &str = "node-cert.pem";
const KEY_FILE: &str = "node-key.pem";

/// PEM material for this node plus metadata parsed from the node cert.
#[derive(Debug, Clone)]
pub struct LoadedPki {
    pub ca_pem: String,
    pub cert_pem: String,
    pub key_pem: String,
    /// Expiry of the node certificate.
    pub not_after: DateTime<Utc>,
    /// Certificate serial as lowercase hex (no leading zeros).
    pub serial: String,
}

/// Generate a fresh P-256 keypair and a PEM PKCS#10 CSR (CN = node id).
/// Returns `(key_pem, csr_pem)`.
pub fn generate_csr(node_id: &str) -> Result<(String, String)> {
    let key = KeyPair::generate().context("generating P-256 key")?;
    let mut params = CertificateParams::new(Vec::<String>::new()).context("csr params")?;
    let mut dn = DistinguishedName::new();
    dn.push(DnType::CommonName, node_id);
    params.distinguished_name = dn;
    let csr = params.serialize_request(&key).context("building CSR")?;
    Ok((key.serialize_pem(), csr.pem().context("encoding CSR")?))
}

/// Atomically write `data` to `path`: tmp file (0600) + fsync + rename.
fn write_file(path: &Path, data: &str) -> Result<()> {
    let tmp = path.with_extension("pem.tmp");
    let _ = fs::remove_file(&tmp);
    let mut f = fs::OpenOptions::new()
        .write(true)
        .create_new(true)
        .mode(0o600)
        .open(&tmp)
        .with_context(|| format!("creating {}", tmp.display()))?;
    f.write_all(data.as_bytes())
        .with_context(|| format!("writing {}", tmp.display()))?;
    f.sync_all()
        .with_context(|| format!("syncing {}", tmp.display()))?;
    drop(f);
    fs::rename(&tmp, path).with_context(|| format!("renaming to {}", path.display()))
}

/// Write the three PEMs into `dir` (created/tightened to 0700), files 0600,
/// each atomically. The key is written last so a complete key implies the
/// cert and CA were already in place.
pub fn write_pki(dir: &Path, ca_pem: &str, cert_pem: &str, key_pem: &str) -> Result<()> {
    fs::DirBuilder::new()
        .recursive(true)
        .mode(0o700)
        .create(dir)
        .with_context(|| format!("creating {}", dir.display()))?;
    fs::set_permissions(dir, fs::Permissions::from_mode(0o700))
        .with_context(|| format!("chmod {}", dir.display()))?;
    write_file(&dir.join(CA_FILE), ca_pem)?;
    write_file(&dir.join(CERT_FILE), cert_pem)?;
    write_file(&dir.join(KEY_FILE), key_pem)?;
    if let Ok(d) = fs::File::open(dir) {
        let _ = d.sync_all();
    }
    Ok(())
}

/// Parse expiry and serial out of a PEM certificate.
fn cert_meta(cert_pem: &str) -> Result<(DateTime<Utc>, String)> {
    let (_, pem) =
        x509_parser::pem::parse_x509_pem(cert_pem.as_bytes()).context("parsing node cert PEM")?;
    let cert = pem.parse_x509().context("parsing node cert DER")?;
    let not_after = Utc
        .timestamp_opt(cert.validity().not_after.timestamp(), 0)
        .single()
        .context("cert not_after out of range")?;
    Ok((not_after, cert.tbs_certificate.serial.to_str_radix(16)))
}

/// Validate that `ca_pem` parses, `key_pem` is a P-256 key, and the key's
/// public half equals the public key in `cert_pem`.
fn validate(ca_pem: &str, cert_pem: &str, key_pem: &str) -> Result<()> {
    let (_, ca) =
        x509_parser::pem::parse_x509_pem(ca_pem.as_bytes()).context("parsing CA PEM")?;
    ca.parse_x509().context("parsing CA DER")?;

    let key = KeyPair::from_pem(key_pem).context("parsing node key")?;
    if !key.is_compatible(&rcgen::PKCS_ECDSA_P256_SHA256) {
        bail!("node key is not ECDSA P-256");
    }
    let (_, pem) =
        x509_parser::pem::parse_x509_pem(cert_pem.as_bytes()).context("parsing node cert PEM")?;
    let cert = pem.parse_x509().context("parsing node cert DER")?;
    if cert.public_key().subject_public_key.data.as_ref() != key.public_key_raw() {
        bail!("node cert public key does not match node key");
    }
    Ok(())
}

fn build(ca_pem: String, cert_pem: String, key_pem: String) -> Result<LoadedPki> {
    let (not_after, serial) = cert_meta(&cert_pem)?;
    Ok(LoadedPki {
        ca_pem,
        cert_pem,
        key_pem,
        not_after,
        serial,
    })
}

/// Load the PEMs from `dir`; `Ok(None)` if any of the three files is missing.
pub fn load_pki(dir: &Path) -> Result<Option<LoadedPki>> {
    let paths = [dir.join(CA_FILE), dir.join(CERT_FILE), dir.join(KEY_FILE)];
    if !paths.iter().all(|p| p.is_file()) {
        return Ok(None);
    }
    let read = |p: &Path| fs::read_to_string(p).with_context(|| format!("reading {}", p.display()));
    let (ca, cert, key) = match (read(&paths[0]), read(&paths[1]), read(&paths[2])) {
        (Ok(a), Ok(b), Ok(c)) => (a, b, c),
        _ => {
            warn!(dir = %dir.display(), "Unreadable node PKI files; re-enrolling");
            return Ok(None);
        }
    };
    match validate(&ca, &cert, &key).and_then(|()| build(ca, cert, key)) {
        Ok(p) => Ok(Some(p)),
        Err(e) => {
            warn!(error = %e, dir = %dir.display(), "Invalid node PKI on disk; re-enrolling");
            Ok(None)
        }
    }
}

/// Load existing PKI from `pki_dir`, or generate a key + CSR, have the panel
/// sign it via `api`, persist the result and return it.
pub async fn ensure_pki(pki_dir: &Path, api: &ApiClient, node_id: &str) -> Result<LoadedPki> {
    if let Some(p) = load_pki(pki_dir)? {
        info!(not_after = %p.not_after, "Loaded existing node PKI");
        return Ok(p);
    }
    let (key_pem, csr_pem) = generate_csr(node_id)?;
    let (ca_pem, cert_pem) = api.request_node_cert(&csr_pem).await?;
    if ca_pem.trim().is_empty() || cert_pem.trim().is_empty() {
        bail!("panel returned an empty certificate");
    }
    validate(&ca_pem, &cert_pem, &key_pem).context("panel-issued certificate is unusable")?;
    let pki = build(ca_pem, cert_pem, key_pem)?;
    write_pki(pki_dir, &pki.ca_pem, &pki.cert_pem, &pki.key_pem)?;
    info!(not_after = %pki.not_after, serial = %pki.serial, "Obtained node certificate from panel");
    Ok(pki)
}

#[cfg(test)]
mod tests {
    use super::*;
    use rcgen::{CertificateParams, KeyPair};
    use std::fs;
    use std::os::unix::fs::PermissionsExt;

    fn tmp(name: &str) -> std::path::PathBuf {
        std::env::temp_dir().join(format!("pki-test-{name}-{}", uuid::Uuid::new_v4()))
    }

    #[test]
    fn generate_csr_has_key() {
        let (key, csr) = generate_csr("node-1").unwrap();
        assert!(key.contains("PRIVATE KEY"));
        assert!(csr.contains("BEGIN CERTIFICATE REQUEST"));
    }

    #[test]
    fn csr_pem_parses_as_pkcs10() {
        use x509_parser::prelude::FromDer;
        let (_, csr) = generate_csr("node-1").unwrap();
        let (_, pem) = x509_parser::pem::parse_x509_pem(csr.as_bytes()).unwrap();
        let (_, req) =
            x509_parser::certification_request::X509CertificationRequest::from_der(&pem.contents)
                .unwrap();
        req.verify_signature().unwrap();
        assert!(req
            .certification_request_info
            .subject
            .to_string()
            .contains("node-1"));
    }

    #[test]
    fn write_and_reload_pki_roundtrips() {
        let key = KeyPair::generate().unwrap();
        let cert = CertificateParams::new(vec!["x".to_string()])
            .unwrap()
            .self_signed(&key)
            .unwrap();
        let dir = tmp("rt").join("pki");
        assert!(load_pki(&dir).unwrap().is_none());
        write_pki(&dir, &cert.pem(), &cert.pem(), &key.serialize_pem()).unwrap();

        let p = load_pki(&dir).unwrap().unwrap();
        assert_eq!(p.cert_pem, cert.pem());
        assert_eq!(p.key_pem, key.serialize_pem());
        assert!(
            !p.serial.is_empty()
                && p.serial
                    .chars()
                    .all(|c| c.is_ascii_hexdigit() && !c.is_uppercase())
        );
        assert!(p.not_after > Utc::now());

        let mode = |n: &str| fs::metadata(dir.join(n)).unwrap().permissions().mode() & 0o777;
        assert_eq!(mode(KEY_FILE), 0o600);
        assert_eq!(
            fs::metadata(&dir).unwrap().permissions().mode() & 0o777,
            0o700
        );
        let _ = fs::remove_dir_all(dir.parent().unwrap());
    }

    fn valid_set(dir: &Path) -> (String, KeyPair) {
        let key = KeyPair::generate().unwrap();
        let cert = CertificateParams::new(vec!["x".to_string()])
            .unwrap()
            .self_signed(&key)
            .unwrap();
        write_pki(dir, &cert.pem(), &cert.pem(), &key.serialize_pem()).unwrap();
        (cert.pem(), key)
    }

    #[test]
    fn corrupt_key_is_treated_as_missing() {
        let dir = tmp("corrupt").join("pki");
        valid_set(&dir);
        fs::write(dir.join(KEY_FILE), "-----BEGIN PRIVATE KEY-----\ntrunc").unwrap();
        assert!(load_pki(&dir).unwrap().is_none());
        let _ = fs::remove_dir_all(dir.parent().unwrap());
    }

    #[test]
    fn mismatched_key_and_cert_is_treated_as_missing() {
        let dir = tmp("mismatch").join("pki");
        valid_set(&dir);
        let other = KeyPair::generate().unwrap();
        fs::write(dir.join(KEY_FILE), other.serialize_pem()).unwrap();
        assert!(load_pki(&dir).unwrap().is_none());
        let _ = fs::remove_dir_all(dir.parent().unwrap());
    }

    #[test]
    fn partial_files_are_treated_as_missing() {
        let dir = tmp("partial").join("pki");
        valid_set(&dir);
        fs::remove_file(dir.join(KEY_FILE)).unwrap();
        assert!(load_pki(&dir).unwrap().is_none());
        fs::remove_file(dir.join(CERT_FILE)).unwrap();
        assert!(load_pki(&dir).unwrap().is_none());
        let _ = fs::remove_dir_all(dir.parent().unwrap());
    }

    #[test]
    fn write_tightens_existing_perms_and_leaves_no_tmp() {
        let dir = tmp("perm").join("pki");
        fs::create_dir_all(&dir).unwrap();
        fs::set_permissions(&dir, fs::Permissions::from_mode(0o755)).unwrap();
        fs::write(dir.join(KEY_FILE), "old").unwrap();
        fs::set_permissions(dir.join(KEY_FILE), fs::Permissions::from_mode(0o644)).unwrap();
        valid_set(&dir);
        let mode = |p: &Path| fs::metadata(p).unwrap().permissions().mode() & 0o777;
        assert_eq!(mode(&dir), 0o700);
        assert_eq!(mode(&dir.join(KEY_FILE)), 0o600);
        assert!(fs::read_dir(&dir)
            .unwrap()
            .all(|e| !e.unwrap().file_name().to_string_lossy().ends_with(".tmp")));
        assert!(load_pki(&dir).unwrap().is_some());
        let _ = fs::remove_dir_all(dir.parent().unwrap());
    }
}
