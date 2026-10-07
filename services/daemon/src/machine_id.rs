//! Stable machine-id for Hytale's encrypted credential store.
//!
//! Hytale (0.6+) encrypts the server's `/auth login` credentials (`auth.enc`)
//! with a key derived from the host's hardware UUID, which it reads from
//! /etc/machine-id, /var/lib/dbus/machine-id, DMI or dmidecode. Slim container
//! images have none of these, so the store fails with "Failed to get hardware
//! UUID for Linux - all methods failed" and logins cannot persist. We keep one
//! id in the data root (so it survives container re-creation; a new id would
//! make existing auth.enc files undecryptable) and install it as machine-id.

use std::path::Path;

use anyhow::{Context, Result};

/// Ensures `machine_id_path` holds a machine-id. An existing non-empty file is
/// left untouched (real hosts have one). Otherwise the id persisted at
/// `persisted_path` is installed, creating it first if needed.
pub fn ensure_machine_id(machine_id_path: &Path, persisted_path: &Path) -> Result<()> {
    if is_valid_id_file(machine_id_path) {
        return Ok(());
    }

    let id = if is_valid_id_file(persisted_path) {
        std::fs::read_to_string(persisted_path)?.trim().to_string()
    } else {
        let id = uuid::Uuid::new_v4().simple().to_string();
        if let Some(dir) = persisted_path.parent() {
            std::fs::create_dir_all(dir)?;
        }
        std::fs::write(persisted_path, format!("{id}\n"))
            .with_context(|| format!("writing {}", persisted_path.display()))?;
        id
    };

    if let Some(dir) = machine_id_path.parent() {
        std::fs::create_dir_all(dir)?;
    }
    std::fs::write(machine_id_path, format!("{id}\n"))
        .with_context(|| format!("writing {}", machine_id_path.display()))?;
    Ok(())
}

/// machine-id format: 32 lowercase hex characters.
fn is_valid_id_file(path: &Path) -> bool {
    match std::fs::read_to_string(path) {
        Ok(s) => {
            let s = s.trim();
            s.len() == 32 && s.chars().all(|c| c.is_ascii_hexdigit())
        }
        Err(_) => false,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::path::PathBuf;
    use std::sync::atomic::{AtomicUsize, Ordering};

    static COUNTER: AtomicUsize = AtomicUsize::new(0);

    fn scratch_dir() -> PathBuf {
        let n = COUNTER.fetch_add(1, Ordering::SeqCst);
        let dir = std::env::temp_dir().join(format!("taledaemon-machineid-{}-{n}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        dir
    }

    #[test]
    fn creates_and_installs_an_id_when_none_exists() {
        let d = scratch_dir();
        let (etc, kept) = (d.join("etc/machine-id"), d.join("data/machine-id"));
        ensure_machine_id(&etc, &kept).unwrap();

        let installed = std::fs::read_to_string(&etc).unwrap();
        assert_eq!(installed.trim().len(), 32);
        assert_eq!(installed, std::fs::read_to_string(&kept).unwrap());
    }

    // A re-created container must get the same id back, or every server's
    // auth.enc becomes undecryptable.
    #[test]
    fn reuses_the_persisted_id_after_the_container_is_recreated() {
        let d = scratch_dir();
        let (etc, kept) = (d.join("etc/machine-id"), d.join("data/machine-id"));
        ensure_machine_id(&etc, &kept).unwrap();
        let first = std::fs::read_to_string(&etc).unwrap();

        std::fs::remove_file(&etc).unwrap(); // fresh container filesystem
        ensure_machine_id(&etc, &kept).unwrap();
        assert_eq!(std::fs::read_to_string(&etc).unwrap(), first);
    }

    #[test]
    fn leaves_a_real_host_machine_id_alone() {
        let d = scratch_dir();
        let (etc, kept) = (d.join("machine-id"), d.join("data/machine-id"));
        std::fs::write(&etc, "0123456789abcdef0123456789abcdef\n").unwrap();
        ensure_machine_id(&etc, &kept).unwrap();
        assert_eq!(std::fs::read_to_string(&etc).unwrap(), "0123456789abcdef0123456789abcdef\n");
        assert!(!kept.exists(), "nothing should be persisted on a real host");
    }
}
