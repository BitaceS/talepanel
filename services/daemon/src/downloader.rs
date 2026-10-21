/// Hytale server file downloader integration.
///
/// Uses the official Hytale Downloader CLI tool to fetch server files and assets.
/// Download URL: https://downloader.hytale.com/hytale-downloader.zip
///
/// The downloader authenticates with your Hytale account via OAuth2 and
/// downloads the correct server build for your game version.
use anyhow::{bail, Context, Result};
use std::path::{Path, PathBuf};
use tokio::io::{AsyncBufReadExt, BufReader};
use tokio::sync::mpsc;
use tracing::{info, warn};

use crate::http_server::safe_fs;

const DOWNLOADER_URL: &str = "https://downloader.hytale.com/hytale-downloader.zip";

/// Represents the result of a completed download operation.
#[derive(Debug)]
pub struct DownloadResult {
    /// Absolute path to the server data directory containing Server/ and Assets.zip
    pub data_path: PathBuf,
    /// The Hytale server version string (from downloader output)
    pub version: String,
}

/// Download the Hytale Downloader CLI into the given directory and return the
/// path to the extracted binary.
///
/// The downloader zip contains a platform-specific binary:
///   Linux:   hytale-downloader
///   Windows: hytale-downloader.exe
pub async fn fetch_downloader_cli(download_dir: &Path) -> Result<PathBuf> {
    info!("Fetching Hytale Downloader CLI from {}", DOWNLOADER_URL);

    tokio::fs::create_dir_all(download_dir)
        .await
        .context("Failed to create downloader directory")?;

    let zip_path = download_dir.join("hytale-downloader.zip");

    // Download the zip file
    let client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(120))
        .user_agent("TaleDaemon/0.1.0")
        .build()
        .context("Failed to build HTTP client")?;

    let resp = client
        .get(DOWNLOADER_URL)
        .send()
        .await
        .context("Failed to download Hytale Downloader CLI")?;

    if !resp.status().is_success() {
        bail!(
            "Hytale Downloader download failed with HTTP {}",
            resp.status()
        );
    }

    let bytes = resp
        .bytes()
        .await
        .context("Failed to read downloader zip bytes")?;

    tokio::fs::write(&zip_path, &bytes)
        .await
        .context("Failed to write downloader zip")?;

    info!(path = %zip_path.display(), "Downloader zip saved");

    // Extract the zip
    let download_dir_owned = download_dir.to_path_buf();
    let zip_path_owned = zip_path.clone();

    tokio::task::spawn_blocking(move || -> Result<()> {
        let file = std::fs::File::open(&zip_path_owned)
            .context("Failed to open downloader zip")?;
        let mut archive = zip::ZipArchive::new(file)
            .context("Failed to parse downloader zip")?;
        // Not ZipArchive::extract: that one recreates symlink entries.
        safe_fs::extract_zip(&mut archive, &download_dir_owned, safe_fs::ExtractLimits::default())
            .map_err(anyhow::Error::msg)
            .context("Failed to extract downloader zip")?;
        Ok(())
    })
    .await
    .context("Zip extraction task panicked")?
    .context("Failed to extract downloader zip")?;

    // Locate the binary — the zip contains platform-specific names:
    //   Linux:   hytale-downloader-linux-amd64
    //   Windows: hytale-downloader.exe
    let binary_candidates: &[&str] = if cfg!(windows) {
        &["hytale-downloader.exe"]
    } else {
        &[
            "hytale-downloader-linux-amd64",
            "hytale-downloader-linux",
            "hytale-downloader",
        ]
    };

    let mut binary_path = None;
    for name in binary_candidates {
        let candidate = download_dir.join(name);
        if candidate.exists() {
            binary_path = Some(candidate);
            break;
        }
        if let Ok(Some(nested)) = find_binary_recursive(download_dir, name) {
            binary_path = Some(nested);
            break;
        }
    }

    let binary_path = match binary_path {
        Some(p) => p,
        None => {
            bail!(
                "Downloader binary not found after extraction in {} (tried: {:?})",
                download_dir.display(),
                binary_candidates
            );
        }
    };

    // Make executable on Unix
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut perms = std::fs::metadata(&binary_path)?.permissions();
        perms.set_mode(0o755);
        std::fs::set_permissions(&binary_path, perms)?;
    }

    info!(path = %binary_path.display(), "Hytale Downloader CLI ready");
    Ok(binary_path)
}

/// Download Hytale server files to `server_data_path` using the downloader CLI.
///
/// `downloader_bin` — path to the hytale-downloader binary
/// `server_data_path` — target directory for this server instance
/// `version` — Hytale version string (e.g. "latest", "1.0.0")
///
/// The downloader handles OAuth2 authentication interactively or via
/// environment variables (HYTALE_EMAIL / HYTALE_PASSWORD) as per QUICKSTART.md.
/// Download Hytale server files, streaming output lines through `log_tx`.
///
/// `log_tx` receives each stdout/stderr line as it appears — including the
/// OAuth2 device-code URL that the user must open in a browser.  Pass `None`
/// if you don't need real-time output.
pub async fn download_server_files(
    downloader_bin: &Path,
    server_data_path: &Path,
    version: &str,
    log_tx: Option<mpsc::UnboundedSender<String>>,
) -> Result<DownloadResult> {
    // Skip download if server JAR already exists (idempotent)
    let jar_path = server_data_path.join("Server").join("HytaleServer.jar");
    if jar_path.exists() {
        info!(
            path = %jar_path.display(),
            "Server JAR already exists — skipping download"
        );
        if let Some(tx) = &log_tx {
            let _ = tx.send("Server files already downloaded — skipping".into());
        }
        return Ok(DownloadResult {
            data_path: server_data_path.to_path_buf(),
            version: version.to_string(),
        });
    }

    info!(
        target = %server_data_path.display(),
        version,
        "Downloading Hytale server files"
    );

    tokio::fs::create_dir_all(server_data_path)
        .await
        .context("Failed to create server data directory")?;

    // Run the downloader CLI
    // Actual CLI flags (from -help output):
    //   -download-path string   Path to download zip to
    //   -patchline string       Patchline to download from (default "release")
    //   -credentials-path string Path to credentials file
    //   -skip-update-check      Skip checking for hytale-downloader updates
    let patchline = if version == "latest" || version.is_empty() {
        "release"
    } else {
        version
    };

    // Store credentials next to the binary so they survive across runs
    let creds_path = downloader_bin
        .parent()
        .unwrap_or(Path::new("/srv/taledaemon/tools"))
        .join(".hytale-downloader-credentials.json");

    let mut child = tokio::process::Command::new(downloader_bin)
        .arg("-download-path")
        .arg(server_data_path)
        .arg("-patchline")
        .arg(patchline)
        .arg("-credentials-path")
        .arg(&creds_path)
        .arg("-skip-update-check")
        .stdout(std::process::Stdio::piped())
        .stderr(std::process::Stdio::piped())
        .spawn()
        .context("Failed to spawn Hytale Downloader CLI")?;

    // Stream stdout and stderr line by line so OAuth URLs show up immediately
    let stdout = child.stdout.take().context("No stdout")?;
    let stderr = child.stderr.take().context("No stderr")?;

    let log_tx_out = log_tx.clone();
    let stdout_task = tokio::spawn(async move {
        let mut lines = BufReader::new(stdout).lines();
        let mut collected = Vec::new();
        while let Ok(Some(line)) = lines.next_line().await {
            info!("[downloader stdout] {}", line);
            if let Some(tx) = &log_tx_out {
                forward_downloader_line(tx, &line);
            }
            collected.push(line);
        }
        collected
    });

    let log_tx_err = log_tx.clone();
    let stderr_task = tokio::spawn(async move {
        let mut lines = BufReader::new(stderr).lines();
        let mut collected = Vec::new();
        while let Ok(Some(line)) = lines.next_line().await {
            info!("[downloader stderr] {}", line);
            if let Some(tx) = &log_tx_err {
                forward_downloader_line(tx, &line);
            }
            collected.push(line);
        }
        collected
    });

    let status = child.wait().await.context("Failed to wait for Hytale Downloader")?;
    let stdout_lines = stdout_task.await.unwrap_or_default();
    let stderr_lines = stderr_task.await.unwrap_or_default();

    let stdout_str = stdout_lines.join("\n");

    if !status.success() {
        let all_output = format!(
            "stdout: {}\nstderr: {}",
            stdout_str,
            stderr_lines.join("\n")
        );
        bail!(
            "Hytale Downloader failed (exit {})\n{}",
            status.code().unwrap_or(-1),
            all_output
        );
    }

    info!("Hytale Downloader completed successfully");

    // The downloader treats `-download-path` as a FILE path and appends `.zip`.
    // So for `-download-path /srv/.../abc`, the output is `/srv/.../abc.zip`.
    // That is the only archive extracted. The server directory itself is
    // tenant-writable, so any *.zip found in there is tenant data, never
    // downloader output.
    let sibling_zip = downloader_output_zip(server_data_path);

    let game_zip = match std::fs::symlink_metadata(&sibling_zip) {
        Ok(md) if md.is_file() => {
            info!(zip = %sibling_zip.display(), "Found downloaded zip at sibling path");
            Some(sibling_zip.clone())
        }
        _ => None,
    };

    if let Some(zip_path) = &game_zip {
        info!(zip = %zip_path.display(), "Extracting downloaded game zip into {}", server_data_path.display());
        if let Some(tx) = &log_tx {
            let _ = tx.send(format!("Extracting {} ...", zip_path.display()));
        }
        let zip_owned = zip_path.clone();
        let extract_dir = server_data_path.to_path_buf();
        tokio::task::spawn_blocking(move || -> Result<()> {
            let file = safe_fs::open_nofollow(&zip_owned)
                .context("Failed to open downloaded game zip")?;
            let mut archive = zip::ZipArchive::new(file)
                .context("Failed to parse downloaded game zip")?;
            // Extracts into the tenant's directory: skip symlink entries,
            // never follow planted links, and cap the decompressed size.
            safe_fs::extract_zip(&mut archive, &extract_dir, safe_fs::ExtractLimits::default())
                .map_err(anyhow::Error::msg)
                .context("Failed to extract downloaded game zip")?;
            Ok(())
        })
        .await
        .context("Game zip extraction task panicked")?
        .context("Failed to extract game zip")?;

        // Clean up the zip to save disk space
        if let Err(e) = tokio::fs::remove_file(zip_path).await {
            warn!("Failed to remove game zip after extraction: {e}");
        }

        info!("Game zip extracted successfully");
    } else {
        warn!("No game zip found after download — expected at {}", sibling_zip.display());
        if let Some(tx) = &log_tx {
            let _ = tx.send("WARNING: No game zip found after download".into());
        }
    }

    // Verify expected files exist after download + extraction
    let server_jar = server_data_path.join("Server").join("HytaleServer.jar");
    let assets_zip = server_data_path.join("Assets.zip");

    if !server_jar.exists() {
        // List what files ARE in the directory for debugging
        let mut found_files = Vec::new();
        if let Ok(mut entries) = tokio::fs::read_dir(server_data_path).await {
            while let Ok(Some(entry)) = entries.next_entry().await {
                found_files.push(entry.file_name().to_string_lossy().to_string());
            }
        }
        let msg = format!(
            "HytaleServer.jar not found at {} after download. Files in dir: {:?}",
            server_jar.display(),
            found_files
        );
        warn!("{}", msg);
        if let Some(tx) = &log_tx {
            let _ = tx.send(msg);
        }
    }
    if !assets_zip.exists() {
        let msg = format!("Assets.zip not found at {} after download", assets_zip.display());
        warn!("{}", msg);
        if let Some(tx) = &log_tx {
            let _ = tx.send(msg);
        }
    }

    // Parse version from downloader output if available
    let downloaded_version = parse_version_from_output(&stdout_str).unwrap_or_else(|| version.to_string());

    info!(
        path = %server_data_path.display(),
        version = %downloaded_version,
        "Hytale server files downloaded successfully"
    );

    Ok(DownloadResult {
        data_path: server_data_path.to_path_buf(),
        version: downloaded_version,
    })
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

/// Where the downloader writes its archive for `-download-path {data_path}`:
/// the sibling `{data_path}.zip`, outside the tenant-writable directory.
fn downloader_output_zip(server_data_path: &Path) -> PathBuf {
    let mut p = server_data_path.as_os_str().to_owned();
    p.push(".zip");
    PathBuf::from(p)
}

// The Hytale Downloader CLI updates its progress bar in place using \r,
// which our line-based reader collects into one giant line that looks like
// "[ ] 0.0% (...) [ ] 1.0% (...) [== ] 2.0% (...) ...".  Forwarding that
// to the panel console produces a single unreadable log entry.
//
// Strategy: split on \r so each progress frame is its own segment, drop
// frames that are pure progress bars, and forward only the final summary
// (e.g. the "100.0% (1.4 GB / 1.4 GB)" line if it's the only thing left,
// or any non-progress text appended at the end).
fn forward_downloader_line(tx: &mpsc::UnboundedSender<String>, line: &str) {
    let segments: Vec<&str> = line.split('\r').map(str::trim).filter(|s| !s.is_empty()).collect();

    if segments.is_empty() {
        return;
    }

    // Multi-segment line — typical when a \r-driven progress bar got merged
    // into a single \n-terminated chunk.  Forward only the last segment, and
    // only if it is not itself a progress frame.
    if segments.len() > 1 {
        let last = segments[segments.len() - 1];
        if !is_progress_frame(last) {
            let _ = tx.send(last.to_string());
        }
        return;
    }

    let only = segments[0];
    if !is_progress_frame(only) {
        let _ = tx.send(only.to_string());
    }
}

// A progress frame from the Hytale Downloader CLI looks like
// "[==========          ] 42.0% (610.4 MB / 1.4 GB)".  Match loosely so we
// also catch the empty-bar opener "[                    ] 0.0% (...)".
fn is_progress_frame(s: &str) -> bool {
    let t = s.trim_start();
    if !t.starts_with('[') {
        return false;
    }
    let Some(close) = t.find(']') else { return false };
    // Inside the brackets the bar uses '=' or ' ' only.
    let inside = &t[1..close];
    if !inside.chars().all(|c| c == '=' || c == ' ') {
        return false;
    }
    // After the bracket we expect a percent number.
    t[close + 1..].contains('%')
}

fn parse_version_from_output(output: &str) -> Option<String> {
    // Look for "version: X.Y.Z" or "Downloaded version X.Y.Z" in the output
    for line in output.lines() {
        let l = line.to_ascii_lowercase();
        if l.contains("version") {
            let parts: Vec<&str> = line.split_whitespace().collect();
            for (i, part) in parts.iter().enumerate() {
                if part.to_ascii_lowercase().contains("version") {
                    if let Some(v) = parts.get(i + 1) {
                        if v.chars().next().map(|c| c.is_ascii_digit()).unwrap_or(false) {
                            return Some(v.trim_matches(',').to_string());
                        }
                    }
                }
            }
        }
    }
    None
}

fn find_binary_recursive(dir: &Path, name: &str) -> Result<Option<PathBuf>> {
    for entry in std::fs::read_dir(dir)? {
        let entry = entry?;
        let path = entry.path();
        if path.is_file() && path.file_name().and_then(|n| n.to_str()) == Some(name) {
            return Ok(Some(path));
        }
        if path.is_dir() {
            if let Some(found) = find_binary_recursive(&path, name)? {
                return Ok(Some(found));
            }
        }
    }
    Ok(None)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn only_the_sibling_zip_is_downloader_output() {
        let out = downloader_output_zip(Path::new("/srv/taledaemon/servers/abc"));
        assert_eq!(out, PathBuf::from("/srv/taledaemon/servers/abc.zip"));
        // Never a path inside the (tenant-writable) server directory.
        assert!(!out.starts_with("/srv/taledaemon/servers/abc/"));
    }
}

