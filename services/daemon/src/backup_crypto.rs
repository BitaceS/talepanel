//! Panel-managed encryption for backup archives.
//!
//! Backups are plain zip files on the node. Before an archive is written to disk
//! (and therefore before it is ever copied off-site), it is encrypted here with a
//! 32-byte key the panel owns and supplies per operation. The key is never
//! persisted on the node.
//!
//! Format: a fixed header — magic `TPBKENC1` (8) + key id (1) + a random nonce
//! prefix (7) — followed by ChaCha20-Poly1305 STREAM chunks (64 KiB plaintext +
//! 16-byte tag each). Restore/download detect the magic and decrypt; archives
//! without it are legacy plaintext zips and pass through untouched.

use std::io::{self, Read, Write};

use base64::Engine;
use chacha20poly1305::aead::generic_array::GenericArray;
use chacha20poly1305::aead::stream::{DecryptorBE32, EncryptorBE32};
use chacha20poly1305::{ChaCha20Poly1305, KeyInit};

/// Leading bytes that mark an encrypted TalePanel backup.
pub const MAGIC: &[u8; 8] = b"TPBKENC1";

/// ChaCha20-Poly1305 STREAM reserves 5 nonce bytes (4 counter + 1 last-block),
/// leaving a 7-byte per-archive random prefix.
const NONCE_PREFIX_LEN: usize = 7;

/// Plaintext chunk size. Each encrypted chunk is this plus the 16-byte tag.
const CHUNK: usize = 64 * 1024;

/// Poly1305 tag length appended to every STREAM chunk.
const TAG: usize = 16;

/// Header length: magic + key id + nonce prefix.
const HEADER_LEN: usize = MAGIC.len() + 1 + NONCE_PREFIX_LEN;

/// True if these leading bytes are an encrypted TalePanel backup (magic match).
pub fn is_encrypted(header: &[u8]) -> bool {
    header.len() >= MAGIC.len() && &header[..MAGIC.len()] == MAGIC
}

/// Decode a base64 32-byte key as sent by the panel.
pub fn decode_key(b64: &str) -> io::Result<[u8; 32]> {
    let bytes = base64::engine::general_purpose::STANDARD
        .decode(b64.trim())
        .map_err(|e| io::Error::new(io::ErrorKind::InvalidData, format!("backup key not valid base64: {e}")))?;
    <[u8; 32]>::try_from(bytes.as_slice())
        .map_err(|_| io::Error::new(io::ErrorKind::InvalidData, "backup key must be 32 bytes"))
}

/// Encrypt everything from `reader` into `writer` under `key`, tagging the
/// stream with `key_id`. Writes the header, then AEAD chunks.
pub fn encrypt_stream<R: Read, W: Write>(
    mut reader: R,
    mut writer: W,
    key: &[u8; 32],
    key_id: u8,
) -> io::Result<()> {
    let mut nonce_prefix = [0u8; NONCE_PREFIX_LEN];
    getrandom::getrandom(&mut nonce_prefix)
        .map_err(|e| io::Error::other(format!("nonce rng failed: {e}")))?;

    writer.write_all(MAGIC)?;
    writer.write_all(&[key_id])?;
    writer.write_all(&nonce_prefix)?;

    let cipher = ChaCha20Poly1305::new(GenericArray::from_slice(key));
    let mut enc = EncryptorBE32::from_aead(cipher, GenericArray::from_slice(&nonce_prefix));

    // Look one chunk ahead so we know which chunk is the last — even when the
    // input is empty or an exact multiple of the chunk size.
    let mut cur = vec![0u8; CHUNK];
    let mut nxt = vec![0u8; CHUNK];
    let mut n = read_full(&mut reader, &mut cur)?;
    loop {
        let m = read_full(&mut reader, &mut nxt)?;
        if m == 0 {
            let ct = enc.encrypt_last(&cur[..n]).map_err(aead_err)?;
            writer.write_all(&ct)?;
            break;
        }
        let ct = enc.encrypt_next(&cur[..n]).map_err(aead_err)?;
        writer.write_all(&ct)?;
        std::mem::swap(&mut cur, &mut nxt);
        n = m;
    }
    writer.flush()
}

/// Decrypt a stream produced by [`encrypt_stream`]. Fails on a wrong key, any
/// tampering, or truncation.
pub fn decrypt_stream<R: Read, W: Write>(
    mut reader: R,
    mut writer: W,
    key: &[u8; 32],
) -> io::Result<()> {
    let mut header = [0u8; HEADER_LEN];
    if read_full(&mut reader, &mut header)? != HEADER_LEN {
        return Err(io::Error::new(io::ErrorKind::UnexpectedEof, "truncated backup header"));
    }
    if !is_encrypted(&header) {
        return Err(io::Error::new(io::ErrorKind::InvalidData, "not an encrypted backup"));
    }
    // header[MAGIC.len()] is the key id; the panel already supplied the matching
    // key, so we only need the nonce prefix that follows it.
    let mut nonce_prefix = [0u8; NONCE_PREFIX_LEN];
    nonce_prefix.copy_from_slice(&header[MAGIC.len() + 1..]);

    let cipher = ChaCha20Poly1305::new(GenericArray::from_slice(key));
    let mut dec = DecryptorBE32::from_aead(cipher, GenericArray::from_slice(&nonce_prefix));

    let enc_chunk = CHUNK + TAG;
    let mut cur = vec![0u8; enc_chunk];
    let mut nxt = vec![0u8; enc_chunk];
    let mut n = read_full(&mut reader, &mut cur)?;
    loop {
        let m = read_full(&mut reader, &mut nxt)?;
        if m == 0 {
            let pt = dec.decrypt_last(&cur[..n]).map_err(aead_err)?;
            writer.write_all(&pt)?;
            break;
        }
        let pt = dec.decrypt_next(&cur[..n]).map_err(aead_err)?;
        writer.write_all(&pt)?;
        std::mem::swap(&mut cur, &mut nxt);
        n = m;
    }
    writer.flush()
}

/// Read until `buf` is full or EOF; returns the number of bytes read.
fn read_full<R: Read>(reader: &mut R, buf: &mut [u8]) -> io::Result<usize> {
    let mut filled = 0;
    while filled < buf.len() {
        match reader.read(&mut buf[filled..]) {
            Ok(0) => break,
            Ok(k) => filled += k,
            Err(ref e) if e.kind() == io::ErrorKind::Interrupted => continue,
            Err(e) => return Err(e),
        }
    }
    Ok(filled)
}

fn aead_err(_e: chacha20poly1305::aead::Error) -> io::Error {
    io::Error::new(io::ErrorKind::InvalidData, "backup archive authentication failed (wrong key or corrupt data)")
}

#[cfg(test)]
mod tests {
    use super::*;

    fn key_a() -> [u8; 32] {
        [7u8; 32]
    }
    fn key_b() -> [u8; 32] {
        [9u8; 32]
    }

    fn roundtrip(plain: &[u8]) -> Vec<u8> {
        let mut ct = Vec::new();
        encrypt_stream(plain, &mut ct, &key_a(), 0).expect("encrypt");
        let mut out = Vec::new();
        decrypt_stream(ct.as_slice(), &mut out, &key_a()).expect("decrypt");
        out
    }

    #[test]
    fn roundtrip_recovers_empty() {
        assert_eq!(roundtrip(b""), b"");
    }

    #[test]
    fn roundtrip_recovers_small() {
        assert_eq!(roundtrip(b"hello hytale world"), b"hello hytale world");
    }

    #[test]
    fn roundtrip_recovers_multi_chunk_and_exact_multiple() {
        // One over a chunk, and an exact multiple of the chunk, both must survive.
        let over = vec![0xABu8; CHUNK + 1];
        assert_eq!(roundtrip(&over), over);
        let exact = vec![0xCDu8; CHUNK * 3];
        assert_eq!(roundtrip(&exact), exact);
    }

    #[test]
    fn output_starts_with_magic() {
        let mut ct = Vec::new();
        encrypt_stream(&b"x"[..], &mut ct, &key_a(), 5).expect("encrypt");
        assert!(is_encrypted(&ct));
        assert_eq!(ct[MAGIC.len()], 5, "key id is stored in the header");
    }

    #[test]
    fn wrong_key_fails() {
        let mut ct = Vec::new();
        encrypt_stream(&b"secret worlds"[..], &mut ct, &key_a(), 0).expect("encrypt");
        let mut out = Vec::new();
        assert!(decrypt_stream(ct.as_slice(), &mut out, &key_b()).is_err());
    }

    #[test]
    fn tampered_ciphertext_fails() {
        let mut ct = Vec::new();
        encrypt_stream(&vec![1u8; 100][..], &mut ct, &key_a(), 0).expect("encrypt");
        let last = ct.len() - 1;
        ct[last] ^= 0xFF;
        let mut out = Vec::new();
        assert!(decrypt_stream(ct.as_slice(), &mut out, &key_a()).is_err());
    }

    #[test]
    fn truncated_stream_fails() {
        let mut ct = Vec::new();
        encrypt_stream(&vec![2u8; CHUNK + 50][..], &mut ct, &key_a(), 0).expect("encrypt");
        ct.truncate(ct.len() - 10); // drop part of the final chunk + tag
        let mut out = Vec::new();
        assert!(decrypt_stream(ct.as_slice(), &mut out, &key_a()).is_err());
    }

    #[test]
    fn is_encrypted_false_for_zip_and_short() {
        assert!(!is_encrypted(b"PK\x03\x04rest-of-zip"));
        assert!(!is_encrypted(b"TPB"));
    }

    #[test]
    fn decode_key_roundtrips_and_rejects_wrong_length() {
        let raw = [3u8; 32];
        let b64 = {
            use base64::Engine;
            base64::engine::general_purpose::STANDARD.encode(raw)
        };
        assert_eq!(decode_key(&b64).unwrap(), raw);
        let short = {
            use base64::Engine;
            base64::engine::general_purpose::STANDARD.encode([1u8; 16])
        };
        assert!(decode_key(&short).is_err());
        assert!(decode_key("not base64!!!").is_err());
    }
}
