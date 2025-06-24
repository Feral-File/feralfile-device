//! Firmware / software updater support for setupd.

use crate::constant;
use anyhow::{Context, Result};
use semver::Version;
use serde::Deserialize;
use std::{process::Stdio, sync::OnceLock, time::Duration};
use tokio::{
    fs,
    io::{AsyncBufReadExt, BufReader},
    process::Command,
    select,
    signal::unix::{SignalKind, signal},
    sync::mpsc,
    time,
};

/// ---------- Cache ----------

static CURRENT_BUILD: OnceLock<RunningBuild> = OnceLock::new();
static REMOTE_VERSIONS: OnceLock<UpstreamVersion> = OnceLock::new();

/// ---------- Public API ----------

pub async fn current_version() -> Result<String> {
    let current = read_branch_and_version().await?;
    Ok(current.version.to_string())
}

pub async fn latest_version() -> Result<String> {
    let current = read_branch_and_version().await?;
    let remote_versions = fetch_remote_version(&current.branch).await?;
    let latest = remote_versions.latest_version;
    Ok(latest.to_string())
}

/// Return `Ok(true)` when the running build is **below** the distributor’s
/// minimum supported version and an update is therefore required.
pub async fn is_update_required() -> Result<bool> {
    let current = read_branch_and_version().await?;
    let remote_versions = fetch_remote_version(&current.branch).await?;
    let min_version = remote_versions.min_version;
    println!(
        "Updater: current={:?}, min={:?}, latest={:?}",
        current.version, min_version, remote_versions.latest_version
    );

    Ok(current.version < min_version)
}

/// Spawn the updater in a background task and return a channel receiver that
/// yields each `[progress] …` payload. The caller can `recv().await` and forward
/// the message however it likes (e.g. to CDP).
pub fn spawn_updater() -> Result<mpsc::Receiver<String>> {
    // 16‑item buffer is enough for human‑speed progress updates; adjust if needed.
    let (tx, rx) = mpsc::channel::<String>(16);

    // Detach the async task; errors are logged.
    tokio::spawn(async move {
        if let Err(e) = run_update_and_send(tx).await {
            eprintln!("updater error: {e}");
        }
    });

    Ok(rx)
}

/// Internal async worker: starts the systemd unit, tails the log file,
/// and forwards each `[progress] …` line into the provided `mpsc::Sender`.
async fn run_update_and_send(tx: mpsc::Sender<String>) -> Result<()> {
    // 1. Start the systemd transient service
    let mut child = Command::new("systemctl")
        .args(["start", "feral-updater-run@setupd.service"])
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .spawn()
        .context("starting updater service with systemctl")?;

    // 2. Open (and create if missing) the log file, then seek to end
    let log_path = constant::UPDATER_PROCESS_LOG_FILE;
    let file = fs::OpenOptions::new()
        .create(true)
        .read(true)
        .open(log_path)
        .await
        .with_context(|| format!("opening {}", log_path))?;
    let mut reader = BufReader::new(file).lines();

    // 3. Tail the file in a loop while child is still alive
    let mut sigint = signal(SignalKind::interrupt())?;
    let mut sigterm = signal(SignalKind::terminate())?;

    loop {
        select! {
            maybe_line = reader.next_line() => {
                match maybe_line? {
                    Some(line) if line.starts_with("[progress]") => {
                      let payload = line.trim_start_matches("[progress]").trim().to_string();
                      // Best‑effort send; ignore if receiver dropped (app shutting down)
                      let _ = tx.send(payload).await;
                    }
                    Some(_) => { /* ignore other lines */ }
                    None => { time::sleep(Duration::from_millis(200)).await; }
                }
            }
            status = child.wait() => {
                eprintln!("Updater service exited with status {:?}", status?);
                break;
            }
            _ = sigint.recv() => break,
            _ = sigterm.recv() => break,
        }
    }
    Ok(())
}

/// ---------- Internal helpers ----------

#[derive(Deserialize)]
struct LocalConfigJSON {
    branch: String,
    version: String,
}

#[derive(Debug, Clone)]
struct RunningBuild {
    branch: String,
    version: Version,
}

async fn read_branch_and_version() -> Result<RunningBuild> {
    if let Some(build) = CURRENT_BUILD.get() {
        return Ok(build.clone());
    }

    let buf = fs::read_to_string(constant::UPDATER_LOCAL_CONFIG_PATH)
        .await
        .context("reading local config")?;
    let cfg: LocalConfigJSON = serde_json::from_str(&buf).context("parsing local config JSON")?;
    let version = Version::parse(&cfg.version).context("parsing local semver")?;
    let build = RunningBuild {
        branch: cfg.branch,
        version,
    };
    CURRENT_BUILD.set(build.clone()).unwrap();
    Ok(build)
}

#[derive(Deserialize)]
struct UpstreamInfo {
    min_version: String,
    latest_version: String,
}

#[derive(Debug, Clone)]
struct UpstreamVersion {
    min_version: Version,
    latest_version: Version,
}

async fn fetch_remote_version(branch: &str) -> Result<UpstreamVersion> {
    if let Some(versions) = REMOTE_VERSIONS.get() {
        return Ok(versions.clone());
    }

    let url = format!("{}{}", constant::UPDATER_UPSTREAM_CONFIG_URL_PREFIX, branch);
    let resp = reqwest::Client::new()
        .get(&url)
        .basic_auth(constant::UPDATER_USERNAME, Some(constant::UPDATER_PASSWORD))
        .send()
        .await
        .with_context(|| format!("fetching {}", url))?;

    if !resp.status().is_success() {
        let status = resp.status();
        let body = resp
            .text()
            .await
            .unwrap_or_else(|_| "Failed to read response body".to_string());
        return Err(anyhow::anyhow!(
            "HTTP {} from distributor at {}: {}",
            status,
            url,
            body
        ));
    }

    let info: UpstreamInfo = resp.json().await.context("decoding distributor JSON")?;
    let versions = UpstreamVersion {
        min_version: Version::parse(&info.min_version).context("parsing upstream semver")?,
        latest_version: Version::parse(&info.latest_version).context("parsing upstream semver")?,
    };
    REMOTE_VERSIONS.set(versions.clone()).unwrap();
    Ok(versions)
}
