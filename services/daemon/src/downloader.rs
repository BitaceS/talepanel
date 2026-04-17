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
        archive
            .extract(&download_dir_owned)
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
                let _ = tx.send(line.clone());
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
                let _ = tx.send(line.clone());
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
    // We check that location first, then fall back to scanning inside the dir.
    let sibling_zip = {
        let mut p = server_data_path.as_os_str().to_owned();
        p.push(".zip");
        PathBuf::from(p)
    };

    let mut game_zip: Option<PathBuf> = None;

    if sibling_zip.exists() {
        info!(zip = %sibling_zip.display(), "Found downloaded zip at sibling path");
        game_zip = Some(sibling_zip.clone());
    } else {
        // Fallback: look inside the directory
        if let Ok(mut entries) = tokio::fs::read_dir(server_data_path).await {
            while let Ok(Some(entry)) = entries.next_entry().await {
                let name = entry.file_name();
                let name_str = name.to_string_lossy();
                if name_str.ends_with(".zip") && name_str != "Assets.zip" {
                    game_zip = Some(entry.path());
                    break;
                }
            }
        }
    }

    if let Some(zip_path) = &game_zip {
        info!(zip = %zip_path.display(), "Extracting downloaded game zip into {}", server_data_path.display());
        if let Some(tx) = &log_tx {
            let _ = tx.send(format!("Extracting {} ...", zip_path.display()));
        }
        let zip_owned = zip_path.clone();
        let extract_dir = server_data_path.to_path_buf();
        tokio::task::spawn_blocking(move || -> Result<()> {
            let file = std::fs::File::open(&zip_owned)
                .context("Failed to open downloaded game zip")?;
            let mut archive = zip::ZipArchive::new(file)
                .context("Failed to parse downloaded game zip")?;
            archive
                .extract(&extract_dir)
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
        warn!("No game zip found after download — expected at {} or inside {}", sibling_zip.display(), server_data_path.display());
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

/// Copy server files from a local Launcher installation instead of downloading.
///
/// Launcher paths:
///   Windows: %APPDATA%\Hytale\install\release\package\game\latest
///   Linux:   $XDG_DATA_HOME/Hytale/install/release/package/game/latest
///   macOS:   ~/Library/Application Support/Hytale/install/release/package/game/latest
pub async fn copy_from_launcher(server_data_path: &Path) -> Result<DownloadResult> {
    let launcher_path = find_launcher_path()?;

    info!(
        from = %launcher_path.display(),
        to   = %server_data_path.display(),
        "Copying Hytale server files from Launcher installation"
    );

    let src_server = launcher_path.join("Server");
    let src_assets = launcher_path.join("Assets.zip");

    if !src_server.exists() {
        bail!(
            "Launcher Server folder not found at {}. Is Hytale installed?",
            src_server.display()
        );
    }

    tokio::fs::create_dir_all(server_data_path).await?;

    // Copy Server/ directory
    let dst_server = server_data_path.join("Server");
    copy_dir_recursive(&src_server, &dst_server)
        .await
        .context("Failed to copy Server directory from Launcher")?;

    // Copy Assets.zip
    if src_assets.exists() {
        tokio::fs::copy(&src_assets, server_data_path.join("Assets.zip"))
            .await
            .context("Failed to copy Assets.zip from Launcher")?;
    } else {
        warn!("Assets.zip not found in Launcher installation — server may not start correctly");
    }

    info!("Launcher copy complete");

    Ok(DownloadResult {
        data_path: server_data_path.to_path_buf(),
        version: "launcher-local".to_string(),
    })
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

fn find_launcher_path() -> Result<PathBuf> {
    #[cfg(target_os = "windows")]
    {
        let appdata = std::env::var("APPDATA")
            .context("APPDATA environment variable not set")?;
        Ok(PathBuf::from(appdata)
            .join("Hytale")
            .join("install")
            .join("release")
            .join("package")
            .join("game")
            .join("latest"))
    }

    #[cfg(target_os = "linux")]
    {
        let base = std::env::var("XDG_DATA_HOME")
            .unwrap_or_else(|_| {
                let home = std::env::var("HOME").unwrap_or_default();
                format!("{home}/.local/share")
            });
        Ok(PathBuf::from(base)
            .join("Hytale")
            .join("install")
            .join("release")
            .join("package")
            .join("game")
            .join("latest"))
    }

    #[cfg(target_os = "macos")]
    {
        let home = std::env::var("HOME").context("HOME not set")?;
        Ok(PathBuf::from(home)
            .join("Library")
            .join("Application Support")
            .join("Hytale")
            .join("install")
            .join("release")
            .join("package")
            .join("game")
            .join("latest"))
    }

    #[cfg(not(any(target_os = "windows", target_os = "linux", target_os = "macos")))]
    bail!("Unsupported platform for Launcher copy")
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

async fn copy_dir_recursive(src: &Path, dst: &Path) -> Result<()> {
    tokio::fs::create_dir_all(dst).await?;
    let mut entries = tokio::fs::read_dir(src).await?;

    while let Some(entry) = entries.next_entry().await? {
        let src_path = entry.path();
        let dst_path = dst.join(entry.file_name());

        if src_path.is_dir() {
            Box::pin(copy_dir_recursive(&src_path, &dst_path)).await?;
        } else {
            tokio::fs::copy(&src_path, &dst_path).await?;
        }
    }
    Ok(())
}
