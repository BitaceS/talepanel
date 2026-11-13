# dev.to / Hashnode – Artikel-Entwurf: mTLS zwischen Go-Panel und Rust-Daemon

## Hinweise (Deutsch)

- **Wo/wann:** dev.to (und als Kopie auf Hashnode mit Canonical-URL), Launch-Plan KW 51. Wenn talepanel.com einen Blog bekommt, dort zuerst veröffentlichen und auf dev.to `canonical_url` setzen.
- **Tags (max. 4 auf dev.to):** `go`, `rust`, `security`, `tls`.
- **Cover-Bild:** einfaches Diagramm Panel ↔ Node mit „CSR“, „cert“, „CN = node UUID“, „CN = panel“.
- **Danach:** Link im TalePanel-Discord `#dev`; optional in r/golang bzw. r/rust nur, wenn dort Artikel-Links erlaubt sind (Sidebar prüfen).
- Alle Code-Ausschnitte sind gekürzte Fassungen des echten Codes in `services/api/internal/pki/pki.go`, `services/api/internal/daemon/client.go`, `services/api/internal/handlers/node_csr.go`, `services/api/internal/services/node_service.go`, `services/daemon/src/pki.rs` und `services/daemon/src/http_server.rs` (Stand v0.12). Vor dem Veröffentlichen gegen den aktuellen Code prüfen.
- Länge: ca. 1 500 Wörter ohne Code.

---

```markdown
---
title: "Mutual TLS between a Go control plane and a Rust agent, without asking users to touch a certificate"
published: false
tags: go, rust, security, tls
canonical_url: <talepanel.com blog URL, if any>
---
```

# Mutual TLS between a Go control plane and a Rust agent, without asking users to touch a certificate

TalePanel is an open-source panel for Hytale game servers. It has two halves: a **panel** (Go API + web UI) that people log into, and a **daemon** (Rust) on every machine that actually runs game servers. The panel tells daemons to start servers, write files, run console commands and restore backups. Whatever can talk to a daemon effectively owns that machine.

Up to v0.11 that link was HTTP plus a per-node bearer token. That's fine on a private network and not fine when the panel and the nodes are on different VPS providers, which is how people actually deploy it. In v0.12 we moved to mutual TLS. This post walks through the design and the parts that were less obvious than expected.

The constraint that shaped everything: **users must not have to configure anything.** Many people install TalePanel with one command on a fresh VPS. "Generate a CA with openssl and copy these three files" was not an option.

## The design in one paragraph

The panel runs its own small private CA. When a daemon starts and has no certificate, it generates a key pair locally, sends a certificate signing request (CSR) to the panel, authenticated with the node token it already has, and gets a certificate back. The private key never leaves the node. Once its TLS listener is up, the daemon says so in its heartbeat, and the panel switches that node from `http` to `https` with its own client certificate. Both sides pin *identity*, not hostnames. The node token is still checked on every request, so a certificate alone or a token alone is not enough.

## The panel CA (Go)

The CA is ECDSA P-256, valid for ten years, and can't sign intermediates:

```go
tmpl := &x509.Certificate{
    SerialNumber:          serial, // random 128-bit
    Subject:               pkix.Name{CommonName: "TalePanel Root CA", Organization: []string{"TalePanel"}},
    NotBefore:             now.Add(-5 * time.Minute), // tolerate slow clocks
    NotAfter:              now.Add(validFor),
    KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
    BasicConstraintsValid: true,
    IsCA:                  true,
    MaxPathLenZero:        true,
}
```

It is created lazily on first use and stored in the panel's settings table, encrypted with the same key that already protects TOTP secrets. Creation is "insert if absent, then re-read", so concurrent first callers converge on one CA instead of each minting their own.

## Signing a CSR: trust the key, nothing else

The CSR endpoint is `POST /api/v1/nodes/:id/csr`. It sits behind the daemon auth middleware, and the handler also checks that the token belongs to the node in the URL. The interesting part is what we take from the CSR, which is only the public key:

```go
// The subject CN and SANs come from the caller, never from the CSR,
// so a daemon cannot claim another node's identity.
func (ca *CA) SignCSR(csrPEM []byte, cn string, sans []string, validFor time.Duration) (...) {
    csr, err := ParseCSR(csrPEM) // also checks the CSR's self-signature
    ...
    if pub, ok := csr.PublicKey.(*ecdsa.PublicKey); !ok || pub.Curve != elliptic.P256() {
        return ..., errors.New("CSR key must be ECDSA P-256")
    }
    return ca.sign(csr.PublicKey, cn, sans, validFor)
}
```

The handler calls it with `cn = nodeID` (the node's UUID from the database) and the node's registered FQDN as SAN. Whatever subject the daemon wrote into its CSR is ignored. Leaf certificates are valid for two years, and the serial is written to the `nodes` row. That serial matters later.

## The daemon side (Rust)

On startup the daemon calls `ensure_pki`: load the key, cert and CA from `/srv/taledaemon/pki`, or generate a fresh P-256 key with `rcgen`, build a CSR with `CN = node id`, and ask the panel to sign it.

Before it trusts anything the panel sends back, it validates the whole set:

```rust
fn validate(ca_pem: &str, cert_pem: &str, key_pem: &str, node_id: &str) -> Result<()> {
    // key is P-256 and its public half matches the cert
    // cert is signed by the stored CA
    // exactly one CN, equal to node_id
    // at least 30 days of validity left
}
```

The same check runs when loading from disk. If anything fails (corrupt file, a cert from an earlier install under another node ID, a CA that was replaced, less than 30 days left), the set is treated as missing and the daemon enrolls again. That's also how renewal works today: a restart within 30 days of expiry gets a new certificate.

Writing the files is boring in the right way: directory `0700`, each file written to a `0600` temp file, `fsync`, then `rename`, with the key written last so a complete key implies the cert and CA are already in place. The `Debug` impl of the struct holding the PEMs redacts the key, so it can't end up in a log line by accident.

## One port, two protocols

Existing installs had panels that only spoke plain HTTP to the daemon on port 8444. We didn't want a second port (more firewall rules for users) or a flag day. So the daemon peeks at the first byte of each connection:

```rust
const TLS_HANDSHAKE_RECORD: u8 = 0x16;

let is_tls = match timeout(FIRST_BYTE_TIMEOUT, stream.peek(&mut first)).await {
    Ok(Ok(0)) => return,
    Ok(Ok(_)) => first[0] == TLS_HANDSHAKE_RECORD,
    _ => return,
};
match (is_tls, acceptor) {
    (true, Some(acceptor)) => { /* rustls handshake, 10 s timeout, then serve */ }
    (false, _) if allow_plain => { /* legacy plaintext, bearer token only */ }
    (true, None) => { /* no PKI loaded yet: close */ }
    (false, _) => { /* "Plaintext connection refused: mTLS required" */ }
}
```

A TLS ClientHello always starts with record type `0x16`; an HTTP request starts with a method name in ASCII. Both the first byte and the handshake have a 10-second timeout, so an idle socket can't hold a task forever. The plaintext path stays on by default for one transition period (`TALEDAEMON_ALLOW_LEGACY_BEARER=1`). Operators who want strict mode set `TALEDAEMON_REQUIRE_MTLS=1` and `TALEDAEMON_ALLOW_LEGACY_BEARER=0`. If strict mode is on and no PKI is loaded, the daemon refuses to start instead of silently serving plaintext.

## The subtle one: chain validation is not enough on the daemon

This is the part that is easy to get wrong. The panel's own client certificate and every node's certificate come from the same CA. Our leaves carry both the `clientAuth` and `serverAuth` extended key usages. So if the daemon only checks "is this client cert signed by the panel CA?", **any node's certificate would be accepted as a client by every other node.** Compromise one game server host and you could drive all of them.

The fix is to pin the client identity inside the handshake. rustls lets you wrap the standard WebPKI verifier:

```rust
const PANEL_CLIENT_CN: &str = "panel";

impl ClientCertVerifier for PanelClientVerifier {
    fn client_auth_mandatory(&self) -> bool { true }

    fn verify_client_cert(&self, end_entity: &CertificateDer<'_>,
                          intermediates: &[CertificateDer<'_>], now: UnixTime)
        -> Result<ClientCertVerified, rustls::Error>
    {
        // chain, expiry, signature, clientAuth EKU
        self.inner.verify_client_cert(end_entity, intermediates, now)?;
        match single_common_name(end_entity) {
            Some(cn) if cn == PANEL_CLIENT_CN => Ok(ClientCertVerified::assertion()),
            _ => Err(rustls::Error::InvalidCertificate(
                rustls::CertificateError::ApplicationVerificationFailure)),
        }
    }
    // signature checks delegate to the inner verifier
}
```

`single_common_name` returns `None` if there's no CN, if it isn't a string, or if there is more than one CN attribute, so "CN=panel, CN=node" doesn't slip through. Because the pin runs in the handshake, a non-panel certificate never reaches the HTTP router. We also pass the `ring` crypto provider explicitly to the rustls builders, so no process-wide default provider needs to be installed.

## Pinning the node on the panel side (Go)

The other direction has the opposite problem. The panel reaches nodes by Docker service names, private IPs, or FQDNs that change. Hostname verification would fail half the time, and the hostname isn't the identity anyway. The node's identity is its UUID, which the CA wrote into the CN.

Go's `tls.Config` has no "verify the chain but not the hostname" switch, so we turn off the built-in verification and do it ourselves in `VerifyConnection`:

```go
return &tls.Config{
    MinVersion:         tls.VersionTLS12,
    Certificates:       []tls.Certificate{clientCert}, // CN=panel
    InsecureSkipVerify: true, // disables only Go's hostname-based check
    VerifyConnection: func(cs tls.ConnectionState) error {
        return verifyNodeCert(cs.PeerCertificates, roots, expectedCN)
    },
}, nil
```

`verifyNodeCert` runs `leaf.Verify` against the panel CA with `KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}`, then compares the CN with the node's UUID. We use `VerifyConnection` rather than `VerifyPeerCertificate` on purpose: `VerifyConnection` also runs on resumed sessions, so turning on a session cache later can't quietly bypass the pin.

`InsecureSkipVerify: true` makes every reviewer flinch, and it should. The comment above it explains exactly what replaces it, and there are tests that present a certificate from another CA, one with the wrong CN, an expired or not-yet-valid one and a client-only one, and expect each to be rejected – plus one that checks the hostname really is ignored.

## Don't lock yourself out: the serial check

The last piece is when the panel actually starts using HTTPS for a node. The daemon's heartbeat includes `mtls_ready` and the serial of the certificate it serves. The panel only flips the node to mTLS if that serial equals the one it recorded when it signed the CSR:

```sql
UPDATE nodes
SET mtls_enabled = mtls_enabled OR COALESCE($4 AND cert_serial = NULLIF($5, ''), false), ...
WHERE id = $3
```

Without this, a node with a leftover certificate from an earlier install (or from another panel) would advertise "TLS ready", the panel would pin it, the handshake would fail, and the node would be unreachable with no obvious reason. With it, a mismatched advert changes nothing and gets logged.

## Testing across two languages

Two implementations of X.509 handling will disagree somewhere. We generate fixtures with the Go package's real signing code (CA, node cert, node key, plus a cert for a different node ID), commit the PEMs under `services/daemon/testdata/go-pki`, and the Rust tests load them through the same `load_pki` path the daemon uses. The fixtures are issued with a 20-year validity so they don't rot in the repo. The Rust tests also cover the unhappy paths: truncated key, mismatched key and cert, cert for another node, expired, near expiry, and a cert not signed by the stored CA.

## What's still missing

We list these in SECURITY.md rather than pretend they're solved:

- **The node token is the enrollment credential.** Whoever has a node's token can get a valid certificate for that node. Removing the node in the panel revokes it, but there's no separate one-time enrollment secret for the CSR step yet.
- **No revocation lists and no renewal while running.** Certificates last two years and are renewed on a restart within 30 days of expiry.
- **No CA rotation** yet.
- **Transition mode is on by default**, so a node still accepts plaintext from older panels until the operator turns it off.

## Takeaways

- If your control plane and agents share a CA, **pin the role** (here: CN) on both sides. Chain validation only tells you "someone we issued a cert to".
- Take only the public key from a CSR; set the subject yourself from authenticated data.
- In Go, `VerifyConnection` is the hook for custom identity checks; it also covers resumed sessions.
- Peeking one byte is enough to serve TLS and legacy HTTP on one port during a migration.
- Make "the agent advertises TLS" conditional on something the control plane issued (the serial), or stale state will lock you out.

TalePanel is AGPL-3.0 and on GitHub: https://github.com/BitaceS/talepanel. The code from this post lives in `services/api/internal/pki`, `services/api/internal/daemon/client.go`, `services/daemon/src/pki.rs` and `services/daemon/src/http_server.rs`. Reviews of the approach are very welcome – security reports go to the address in SECURITY.md.
