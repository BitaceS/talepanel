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

use crate::api_client::{ApiClient, NodeCertResponse};

const CA_FILE: &str = "ca.pem";
const CERT_FILE: &str = "node-cert.pem";
const KEY_FILE: &str = "node-key.pem";

/// A node cert with less than this left is treated as missing, so the daemon
/// re-enrols (and gets a fresh cert) instead of serving one about to expire.
const MIN_REMAINING_DAYS: i64 = 30;

/// PEM material for this node plus metadata parsed from the node cert.
#[derive(Clone)]
pub struct LoadedPki {
    pub ca_pem: String,
    pub cert_pem: String,
    pub key_pem: String,
    /// Expiry of the node certificate.
    pub not_after: DateTime<Utc>,
    /// Certificate serial as lowercase hex (no leading zeros).
    pub serial: String,
}

/// Manual impl so the private key never ends up in logs.
impl std::fmt::Debug for LoadedPki {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("LoadedPki")
            .field("ca_pem", &self.ca_pem)
            .field("cert_pem", &self.cert_pem)
            .field("key_pem", &"<redacted>")
            .field("not_after", &self.not_after)
            .field("serial", &self.serial)
            .finish()
    }
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

/// DER of every CERTIFICATE block in a PEM bundle.
fn bundle_ders(pem: &str) -> Result<Vec<Vec<u8>>> {
    let mut out = Vec::new();
    for block in x509_parser::pem::Pem::iter_from_buffer(pem.as_bytes()) {
        let block = block.context("parsing CA bundle PEM")?;
        if block.label == "CERTIFICATE" {
            out.push(block.contents);
        }
    }
    if out.is_empty() {
        bail!("CA bundle contains no certificate");
    }
    Ok(out)
}

fn parse_der(der: &[u8]) -> Result<x509_parser::certificate::X509Certificate<'_>> {
    use x509_parser::prelude::FromDer;
    let (_, c) = x509_parser::certificate::X509Certificate::from_der(der)
        .map_err(|e| anyhow::anyhow!("parsing certificate DER: {e}"))?;
    Ok(c)
}

/// True if `cert` is signed by one of the CAs in `ca_ders`.
fn signed_by_any(
    cert: &x509_parser::certificate::X509Certificate<'_>,
    ca_ders: &[Vec<u8>],
) -> bool {
    ca_ders.iter().any(|der| {
        parse_der(der).is_ok_and(|ca| cert.verify_signature(Some(ca.public_key())).is_ok())
    })
}

/// Check that `new_bundle` may replace `current_bundle` as this node's trust.
///
/// Dropping CAs is always allowed. A CA that is not trusted yet is accepted
/// only with an endorsement: a currently valid cross certificate in
/// `endorsements` with the new CA's subject and public key, signed by a CA
/// this node already trusts. So a new root can only come from the panel that
/// holds the old CA's key, not from whoever answers on the API address.
pub fn accept_bundle(current_bundle: &str, new_bundle: &str, endorsements: &str) -> Result<()> {
    let current = bundle_ders(current_bundle)?;
    let incoming = bundle_ders(new_bundle)?;
    let cross: Vec<Vec<u8>> = if endorsements.trim().is_empty() {
        Vec::new()
    } else {
        bundle_ders(endorsements)?
    };
    for der in &incoming {
        let ca = parse_der(der)?;
        if !ca.is_ca() {
            bail!("CA bundle entry {} is not a CA certificate", ca.subject());
        }
        if current.contains(der) {
            continue;
        }
        let endorsed = cross.iter().any(|e| {
            parse_der(e).is_ok_and(|e| {
                e.subject().as_raw() == ca.subject().as_raw()
                    && e.public_key().raw == ca.public_key().raw
                    && e.validity().is_valid()
                    && signed_by_any(&e, &current)
            })
        });
        if !endorsed {
            bail!(
                "new panel CA {} is not endorsed by a CA this node trusts; \
                 re-enrol the node (delete the PKI directory and restart the daemon)",
                ca.subject()
            );
        }
    }
    Ok(())
}

/// Validate the PKI set for `node_id`: `ca_pem` (a bundle) parses, `key_pem`
/// is a P-256 key whose public half equals the public key in `cert_pem`, the
/// cert is signed by one of the bundle's CAs, its subject CN is `node_id`,
/// and it is valid for at least [`MIN_REMAINING_DAYS`] more days.
///
/// A set failing any check is stale (left over from an earlier install with
/// another node id, issued by another panel, expired) and must be re-enrolled
/// rather than served, or the panel would pin a cert the daemon cannot present.
fn validate(ca_pem: &str, cert_pem: &str, key_pem: &str, node_id: &str) -> Result<()> {
    let cas = bundle_ders(ca_pem)?;

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
    if !signed_by_any(&cert, &cas) {
        bail!("node cert is not signed by a stored CA");
    }

    let mut cns = cert.subject().iter_common_name();
    let cn = cns.next().and_then(|a| a.as_str().ok());
    if cns.next().is_some() || !cn.is_some_and(|cn| cn.eq_ignore_ascii_case(node_id)) {
        bail!(
            "node cert CN {:?} does not match configured node id {node_id}",
            cn.unwrap_or("")
        );
    }

    let (not_after, _) = cert_meta(cert_pem)?;
    if not_after - Utc::now() < chrono::Duration::days(MIN_REMAINING_DAYS) {
        bail!("node cert expires at {not_after} (less than {MIN_REMAINING_DAYS} days left)");
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

/// Load the PEMs from `dir` for `node_id`; `Ok(None)` if any of the three
/// files is missing or the set fails [`validate`] (stale, foreign, expiring).
pub fn load_pki(dir: &Path, node_id: &str) -> Result<Option<LoadedPki>> {
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
    match validate(&ca, &cert, &key, node_id).and_then(|()| build(ca, cert, key)) {
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
    if let Some(p) = load_pki(pki_dir, node_id)? {
        info!(not_after = %p.not_after, "Loaded existing node PKI");
        return Ok(p);
    }
    let (key_pem, csr_pem) = generate_csr(node_id)?;
    let resp = api.request_node_cert(&csr_pem).await?;
    let pki = issued(resp, key_pem, node_id)?;
    write_pki(pki_dir, &pki.ca_pem, &pki.cert_pem, &pki.key_pem)?;
    info!(not_after = %pki.not_after, serial = %pki.serial, "Obtained node certificate from panel");
    Ok(pki)
}

/// Check and assemble a panel CSR answer for the key that made the CSR.
fn issued(resp: NodeCertResponse, key_pem: String, node_id: &str) -> Result<LoadedPki> {
    if resp.ca_pem.trim().is_empty() || resp.cert_pem.trim().is_empty() {
        bail!("panel returned an empty certificate");
    }
    validate(&resp.ca_pem, &resp.cert_pem, &key_pem, node_id)
        .context("panel-issued certificate is unusable")?;
    build(resp.ca_pem, resp.cert_pem, key_pem)
}

/// True when the certificate is inside the renewal window.
pub fn needs_renewal(p: &LoadedPki) -> bool {
    p.not_after - Utc::now() < chrono::Duration::days(MIN_REMAINING_DAYS)
}

/// Get a new key and certificate while running. The panel's answer must be a
/// trust bundle this node may switch to (see [`accept_bundle`]). Nothing is
/// written; the caller persists the result once the listener accepted it.
pub async fn renew(api: &ApiClient, node_id: &str, current: &LoadedPki) -> Result<LoadedPki> {
    let (key_pem, csr_pem) = generate_csr(node_id)?;
    let resp = api.request_node_cert(&csr_pem).await?;
    accept_bundle(&current.ca_pem, &resp.ca_pem, &resp.endorsements_pem)?;
    issued(resp, key_pem, node_id)
}

/// The PKI with `bundle` as trust, if it differs from the current trust, the
/// node may switch to it and its own certificate still chains to it.
/// `Ok(None)` means nothing to change.
pub fn with_trust(
    current: &LoadedPki,
    bundle: &str,
    endorsements: &str,
) -> Result<Option<LoadedPki>> {
    if bundle_ders(bundle)? == bundle_ders(&current.ca_pem)? {
        return Ok(None);
    }
    accept_bundle(&current.ca_pem, bundle, endorsements)?;
    let (_, pem) = x509_parser::pem::parse_x509_pem(current.cert_pem.as_bytes())
        .context("parsing node cert PEM")?;
    let cert = pem.parse_x509().context("parsing node cert DER")?;
    if !signed_by_any(&cert, &bundle_ders(bundle)?) {
        bail!("the new trust bundle no longer contains the CA of this node's certificate");
    }
    Ok(Some(LoadedPki {
        ca_pem: bundle.to_string(),
        ..current.clone()
    }))
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

    const NODE: &str = "11111111-2222-3333-4444-555555555555";

    /// Self-signed cert (acting as its own CA) with subject CN `cn`, valid
    /// until `not_after` as (year, month, day); rcgen's far-future default
    /// when `None`. Returns `(cert_pem, key)`.
    fn self_signed(cn: &str, not_after: Option<(i32, u8, u8)>) -> (String, KeyPair) {
        let key = KeyPair::generate().unwrap();
        let mut params = CertificateParams::new(vec!["x".to_string()]).unwrap();
        params
            .distinguished_name
            .push(rcgen::DnType::CommonName, cn);
        if let Some((y, m, d)) = not_after {
            params.not_after = rcgen::date_time_ymd(y, m, d);
        }
        (params.self_signed(&key).unwrap().pem(), key)
    }

    fn in_days(days: i64) -> (i32, u8, u8) {
        use chrono::Datelike;
        let d = Utc::now() + chrono::Duration::days(days);
        (d.year(), d.month() as u8, d.day() as u8)
    }

    #[test]
    fn write_and_reload_pki_roundtrips() {
        let (cert, key) = self_signed(NODE, None);
        let dir = tmp("rt").join("pki");
        assert!(load_pki(&dir, NODE).unwrap().is_none());
        write_pki(&dir, &cert, &cert, &key.serialize_pem()).unwrap();

        let p = load_pki(&dir, NODE).unwrap().unwrap();
        assert_eq!(p.cert_pem, cert);
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
        let (cert, key) = self_signed(NODE, None);
        write_pki(dir, &cert, &cert, &key.serialize_pem()).unwrap();
        (cert, key)
    }

    #[test]
    fn corrupt_key_is_treated_as_missing() {
        let dir = tmp("corrupt").join("pki");
        valid_set(&dir);
        fs::write(dir.join(KEY_FILE), "-----BEGIN PRIVATE KEY-----\ntrunc").unwrap();
        assert!(load_pki(&dir, NODE).unwrap().is_none());
        let _ = fs::remove_dir_all(dir.parent().unwrap());
    }

    #[test]
    fn mismatched_key_and_cert_is_treated_as_missing() {
        let dir = tmp("mismatch").join("pki");
        valid_set(&dir);
        let other = KeyPair::generate().unwrap();
        fs::write(dir.join(KEY_FILE), other.serialize_pem()).unwrap();
        assert!(load_pki(&dir, NODE).unwrap().is_none());
        let _ = fs::remove_dir_all(dir.parent().unwrap());
    }

    #[test]
    fn partial_files_are_treated_as_missing() {
        let dir = tmp("partial").join("pki");
        valid_set(&dir);
        fs::remove_file(dir.join(KEY_FILE)).unwrap();
        assert!(load_pki(&dir, NODE).unwrap().is_none());
        fs::remove_file(dir.join(CERT_FILE)).unwrap();
        assert!(load_pki(&dir, NODE).unwrap().is_none());
        let _ = fs::remove_dir_all(dir.parent().unwrap());
    }

    #[test]
    fn cert_for_another_node_id_is_treated_as_missing() {
        // Left over from an earlier install that was enrolled as another node.
        let dir = tmp("other-node").join("pki");
        valid_set(&dir);
        assert!(load_pki(&dir, NODE).unwrap().is_some());
        assert!(load_pki(&dir, "99999999-8888-7777-6666-555555555555")
            .unwrap()
            .is_none());
        let _ = fs::remove_dir_all(dir.parent().unwrap());
    }

    #[test]
    fn expired_cert_is_treated_as_missing() {
        let dir = tmp("expired").join("pki");
        let (cert, key) = self_signed(NODE, Some((2020, 1, 1)));
        write_pki(&dir, &cert, &cert, &key.serialize_pem()).unwrap();
        assert!(load_pki(&dir, NODE).unwrap().is_none());
        let _ = fs::remove_dir_all(dir.parent().unwrap());
    }

    #[test]
    fn cert_near_expiry_is_treated_as_missing() {
        let dir = tmp("near-expiry").join("pki");
        let (cert, key) = self_signed(NODE, Some(in_days(10)));
        write_pki(&dir, &cert, &cert, &key.serialize_pem()).unwrap();
        assert!(load_pki(&dir, NODE).unwrap().is_none());

        // Comfortably outside the renewal window: kept.
        let (cert, key) = self_signed(NODE, Some(in_days(60)));
        write_pki(&dir, &cert, &cert, &key.serialize_pem()).unwrap();
        assert!(load_pki(&dir, NODE).unwrap().is_some());
        let _ = fs::remove_dir_all(dir.parent().unwrap());
    }

    #[test]
    fn cert_not_signed_by_stored_ca_is_treated_as_missing() {
        // e.g. the panel's CA was replaced; the stored CA is the new one.
        let dir = tmp("foreign-ca").join("pki");
        let (cert, key) = self_signed(NODE, None);
        let (other_ca, _) = self_signed("TalePanel Root CA", None);
        write_pki(&dir, &other_ca, &cert, &key.serialize_pem()).unwrap();
        assert!(load_pki(&dir, NODE).unwrap().is_none());
        let _ = fs::remove_dir_all(dir.parent().unwrap());
    }

    #[test]
    fn go_issued_pki_loads() {
        // Cross-language: PEMs issued by the Go panel's pki package
        // (see testdata/README.md).
        let dir = tmp("go").join("pki");
        write_pki(
            &dir,
            include_str!("../testdata/go-pki/ca.pem"),
            include_str!("../testdata/go-pki/node-cert.pem"),
            include_str!("../testdata/go-pki/node-key.pem"),
        )
        .unwrap();
        let p = load_pki(&dir, NODE)
            .unwrap()
            .expect("Go-issued PKI rejected");
        assert!(p.not_after > Utc::now() + chrono::Duration::days(365));
        let _ = fs::remove_dir_all(dir.parent().unwrap());
    }

    /// A CA (`CN=name`) and its key.
    fn ca(name: &str) -> (rcgen::Certificate, KeyPair) {
        let key = KeyPair::generate().unwrap();
        let mut p = CertificateParams::new(Vec::<String>::new()).unwrap();
        p.distinguished_name.push(rcgen::DnType::CommonName, name);
        p.is_ca = rcgen::IsCa::Ca(rcgen::BasicConstraints::Unconstrained);
        (p.self_signed(&key).unwrap(), key)
    }

    /// Cross certificate: `subject_ca`'s name and key, signed by `issuer`.
    fn endorse(
        subject_name: &str,
        subject_key: &KeyPair,
        issuer: &rcgen::Certificate,
        issuer_key: &KeyPair,
    ) -> String {
        let mut p = CertificateParams::new(Vec::<String>::new()).unwrap();
        p.distinguished_name
            .push(rcgen::DnType::CommonName, subject_name);
        p.is_ca = rcgen::IsCa::Ca(rcgen::BasicConstraints::Unconstrained);
        p.signed_by(subject_key, issuer, issuer_key).unwrap().pem()
    }

    /// Node cert for NODE signed by `issuer`, plus its key.
    fn node_cert(issuer: &rcgen::Certificate, issuer_key: &KeyPair) -> (String, KeyPair) {
        let key = KeyPair::generate().unwrap();
        let mut p = CertificateParams::new(vec!["x".to_string()]).unwrap();
        p.distinguished_name.push(rcgen::DnType::CommonName, NODE);
        (p.signed_by(&key, issuer, issuer_key).unwrap().pem(), key)
    }

    #[test]
    fn bundle_accepts_endorsed_new_ca_only() {
        let (old, old_key) = ca("TalePanel Root CA");
        let (new, new_key) = ca("TalePanel Root CA 2");
        let (rogue, rogue_key) = ca("TalePanel Root CA 2");
        let bundle = format!("{}{}", new.pem(), old.pem());

        // No endorsement: refused.
        assert!(accept_bundle(&old.pem(), &bundle, "").is_err());
        // Endorsed by the trusted old CA: accepted.
        let cross = endorse("TalePanel Root CA 2", &new_key, &old, &old_key);
        accept_bundle(&old.pem(), &bundle, &cross).unwrap();
        // Endorsement for another key with the same name does not count.
        let rogue_bundle = format!("{}{}", rogue.pem(), old.pem());
        assert!(accept_bundle(&old.pem(), &rogue_bundle, &cross).is_err());
        // An endorsement signed by an untrusted CA does not count either.
        let self_cross = endorse("TalePanel Root CA 2", &rogue_key, &rogue, &rogue_key);
        assert!(accept_bundle(&old.pem(), &rogue_bundle, &self_cross).is_err());
        // Dropping a CA needs nothing.
        accept_bundle(&bundle, &new.pem(), "").unwrap();
        // A leaf in the bundle is refused.
        let (leaf, _) = node_cert(&old, &old_key);
        assert!(accept_bundle(&old.pem(), &format!("{}{leaf}", old.pem()), "").is_err());
    }

    #[test]
    fn node_cert_from_any_bundle_ca_validates() {
        let (old, old_key) = ca("TalePanel Root CA");
        let (new, new_key) = ca("TalePanel Root CA 2");
        let (cert, key) = node_cert(&new, &new_key);
        let bundle = format!("{}{}", old.pem(), new.pem());
        validate(&bundle, &cert, &key.serialize_pem(), NODE).unwrap();
        assert!(validate(&old.pem(), &cert, &key.serialize_pem(), NODE).is_err());
        let _ = old_key;
    }

    #[test]
    fn with_trust_switches_only_when_safe() {
        let (old, old_key) = ca("TalePanel Root CA");
        let (new, new_key) = ca("TalePanel Root CA 2");
        let (cert, key) = node_cert(&old, &old_key);
        let current = build(old.pem(), cert, key.serialize_pem()).unwrap();

        // Same bundle: nothing to do.
        assert!(with_trust(&current, &old.pem(), "").unwrap().is_none());
        // New CA added with endorsement: switch.
        let bundle = format!("{}{}", new.pem(), old.pem());
        let cross = endorse("TalePanel Root CA 2", &new_key, &old, &old_key);
        let next = with_trust(&current, &bundle, &cross).unwrap().unwrap();
        assert_eq!(next.ca_pem, bundle);
        assert_eq!(next.serial, current.serial);
        // Dropping the CA of our own cert is refused (renew first).
        assert!(with_trust(&next, &new.pem(), "").is_err());
    }

    #[test]
    fn debug_redacts_private_key() {
        let (cert, key) = self_signed(NODE, None);
        let key_pem = key.serialize_pem();
        let pki = LoadedPki {
            ca_pem: cert.clone(),
            cert_pem: cert,
            key_pem: key_pem.clone(),
            not_after: Utc::now(),
            serial: "1".into(),
        };
        let out = format!("{pki:?}");
        assert!(
            !out.contains(&key_pem) && !out.contains("PRIVATE KEY"),
            "{out}"
        );
        assert!(out.contains("<redacted>"));
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
        assert!(fs::read_dir(&dir).unwrap().all(|e| !e
            .unwrap()
            .file_name()
            .to_string_lossy()
            .ends_with(".tmp")));
        assert!(load_pki(&dir, NODE).unwrap().is_some());
        let _ = fs::remove_dir_all(dir.parent().unwrap());
    }
}
