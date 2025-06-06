use std::collections::HashSet;
use std::error::Error;
use std::process::Command;
use std::sync::Arc;
use std::time::{Duration, Instant};
use tokio::sync::{Mutex, Notify};
use tokio::task;

use crate::constant;

struct State {
    cached_ssids: Vec<String>,
    expired_at: Option<Instant>,
    refreshing: bool,
}

pub struct SSIDsCacher {
    state: Arc<Mutex<State>>,
    notify: Arc<Notify>,
}

impl SSIDsCacher {
    pub fn new() -> Self {
        Self {
            state: Arc::new(Mutex::new(State {
                cached_ssids: Vec::with_capacity(constant::MAX_SSIDS),
                expired_at: None,
                refreshing: false,
            })),
            notify: Arc::new(Notify::new()),
        }
    }

    /// Refresh and forget
    pub fn trigger_refresh(&self) {
        let state = Arc::clone(&self.state);
        let notify = Arc::clone(&self.notify);

        task::spawn(async move {
            {
                let mut st = state.lock().await;
                if st.refreshing {
                    // someone else is doing it, we don't need to do anything
                    return;
                }
                // We need to set the flag to avoid concurrent refreshing
                st.refreshing = true;
            }

            println!("SSIDsCacher: refreshing...");
            let res = list_ssids(true).await;
            println!("SSIDsCacher: refreshed: \n{:?}", res);

            {
                let mut st = state.lock().await;
                if let Ok(ssids) = res {
                    st.cached_ssids = ssids;
                    st.expired_at =
                        Some(Instant::now() + Duration::from_millis(constant::SSID_CACHE_TTL));
                }
                st.refreshing = false;
            }

            // wake everyone waiting in `get()`
            notify.notify_waiters();
        });
    }

    /// Get SSIDs, waiting only if a refresh is currently in progress or required.
    pub async fn get(&self) -> Result<Vec<String>, Box<dyn Error>> {
        loop {
            {
                let st = self.state.lock().await;

                // Fast path: fresh cache
                if let Some(exp) = st.expired_at {
                    if exp > Instant::now() {
                        println!("SSIDsCacher: returning cached SSIDs");
                        return Ok(st.cached_ssids.clone());
                    }
                }
                println!("SSIDsCacher: no cached or expired SSIDs");

                // If no refresh is underway, kick one off.
                if !st.refreshing {
                    println!("SSIDsCacher: triggering refresh");
                    drop(st); // release lock before spawning
                    self.trigger_refresh();
                }
                // else: someone else is refreshing → fall through to wait
            }

            // Wait until the background task signals completion.
            self.notify.notified().await;
        }
    }
}

pub fn connect(ssid: &str, pass: &str) -> Result<(), Box<dyn Error + Send + Sync>> {
    // delete any existing connection, don't care if it fails
    // we need this because of a bug with nmcli
    // https://bbs.archlinux.org/viewtopic.php?id=300321&p=2

    if let Err(err) = delete(ssid) {
        println!("Wifi: failed to delete existing connection: {}", err);
    }

    println!("Wifi: connecting to {}", ssid);
    let output = Command::new("nmcli")
        .args(&["device", "wifi", "connect", ssid, "password", pass])
        .output();
    println!("Wifi: output: {:?}", output);

    if output.is_err() {
        return Err(format!("Wifi: failed to call nmcli: {}", output.err().unwrap()).into());
    } else {
        let output = output.unwrap();
        if output.status.success() {
            Ok(())
        } else {
            Err(format!(
                "Wifi: failed to connect to {}: {}",
                ssid,
                String::from_utf8_lossy(&output.stderr)
            )
            .into())
        }
    }
}

fn delete(ssid: &str) -> Result<(), Box<dyn Error + Send + Sync>> {
    if Command::new("nmcli")
        .args(&["connection", "delete", ssid])
        .status()
        .map(|s| s.success())
        .unwrap_or(false)
    {
        Ok(())
    } else {
        Err("Failed to delete connection".into())
    }
}

pub async fn list_ssids(force: bool) -> Result<Vec<String>, Box<dyn Error + Send + Sync>> {
    let output = task::spawn_blocking(move || {
        let mut args = vec!["-t", "-f", "SSID", "device", "wifi", "list"];
        if force {
            args.push("--rescan");
            args.push("yes");
        }
        Command::new("nmcli").args(&args).output()
    })
    .await??;

    // Parse stdout lines, filtering out empty entries
    let stdout = String::from_utf8_lossy(&output.stdout);
    let mut ssids = Vec::new();

    // Keep track of seen SSIDs to avoid duplicates while preserving order
    let mut seen = HashSet::new();

    // Limit to maximum 9 SSIDs
    for line in stdout.lines() {
        if !line.is_empty() && !seen.contains(line) {
            seen.insert(line.to_string());
            ssids.push(line.to_string());

            // Stop once we have 9 SSIDs
            if ssids.len() >= constant::MAX_SSIDS {
                break;
            }
        }
    }
    Ok(ssids)
}
