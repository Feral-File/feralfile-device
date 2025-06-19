//! Firmware / software updater support for setupd.

use crate::constant;
use anyhow::{Context, Result};
use semver::Version;
use serde::Deserialize;
use std::{path::Path, process::Stdio, time::Duration};
use tokio::{
    fs,
    io::{AsyncBufReadExt, BufReader},
    process::Command,
    select,
    signal::unix::{SignalKind, signal},
    sync::mpsc,
    time,
};

/// ---------- Public API ----------

/// Return `Ok(true)` when the running build is **below** the distributor’s
/// minimum supported version and an update is therefore required.
pub async fn is_update_required() -> Result<bool> {
    let current = read_branch_and_version(constant::UPDATER_LOCAL_CONFIG_PATH).await?;
    let latest = fetch_min_version(&current.branch).await?;
    println!(
        "Updater: current={:?}, latest={:?}",
        current.version, latest
    );

    Ok(current.version < latest)
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
        .args(["start", "feral-updater@00:00.service"])
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
struct LocalConfig {
    branch: String,
    version: String,
}

struct RunningBuild {
    branch: String,
    version: Version,
}

async fn read_branch_and_version<P: AsRef<Path>>(config_path: P) -> Result<RunningBuild> {
    let buf = fs::read_to_string(&config_path)
        .await
        .with_context(|| format!("reading {}", config_path.as_ref().display()))?;
    let cfg: LocalConfig = serde_json::from_str(&buf).context("parsing local config JSON")?;
    let version = Version::parse(&cfg.version).context("parsing local semver")?;
    Ok(RunningBuild {
        branch: cfg.branch,
        version,
    })
}

#[derive(Deserialize)]
struct UpstreamInfo {
    min_version: String,
}

async fn fetch_min_version(branch: &str) -> Result<Version> {
    let url = format!("{}{}", constant::UPDATER_UPSTREAM_CONFIG_URL_PREFIX, branch);
    let resp = reqwest::get(&url)
        .await
        .with_context(|| format!("fetching {}", url))?
        .error_for_status()
        .context("non-200 from distributor")?;
    let info: UpstreamInfo = resp.json().await.context("decoding distributor JSON")?;
    Version::parse(&info.min_version).context("parsing upstream semver")
}
