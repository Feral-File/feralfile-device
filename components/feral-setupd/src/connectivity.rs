//! connectivity.rs
//! Drop-in helper for “am I online?” logic.

use crate::dbus_utils;
use std::{sync::Arc, time::Duration};
use tokio::{
    sync::{Mutex, watch},
    task, time,
};

/// Clone-able handle you keep in `AppState`.
#[derive(Clone)]
pub struct Connectivity {
    inner: Arc<Inner>,
}

struct Inner {
    tx: watch::Sender<bool>,   // authoritative state
    rx: watch::Receiver<bool>, // everybody listens on this
    /// Protects the *forced* DBus call so we never launch two at once.
    force_lock: Mutex<()>,
}

impl Connectivity {
    // ---------------------------------------------------------------------
    // Construction
    // ---------------------------------------------------------------------
    /// Spawns the background refresher and returns a usable handle.
    pub async fn spawn() -> Self {
        let initial = dbus_utils::internet_availability();
        let (tx, rx) = watch::channel(initial);

        let inner = Arc::new(Inner {
            tx: tx.clone(),
            rx,
            force_lock: Mutex::new(()),
        });

        // Kick off the 30-second poller
        tokio::spawn(background_refresher(tx));

        Self { inner }
    }

    // ---------------------------------------------------------------------
    // Public API
    // ---------------------------------------------------------------------

    /// Returns the cached value unless `force_refresh == true`.
    ///
    /// * When `force_refresh` is **false** (typical case) it is *zero-cost*.
    /// * When `force_refresh` is **true** we synchronously call DBus,
    ///   update the cache, then return the fresh result.
    pub async fn is_online(&self, force_refresh: bool) -> bool {
        if !force_refresh {
            return *self.inner.rx.borrow();
        }

        // Serialize concurrent “force” calls.
        let _guard = self.inner.force_lock.lock().await;
        let fresh = dbus_utils::internet_availability();
        let _ = self.inner.tx.send_if_modified(|old| {
            if *old != fresh {
                *old = fresh;
                true
            } else {
                false
            }
        });
        fresh
    }

    /// Suspends until the state flips to *online*.
    pub async fn wait_until_online(&self) {
        loop {
            if *self.inner.rx.borrow() {
                return;
            }
            // `.changed()` wakes the moment *any* update is pushed.
            // If the sender is dropped `changed()` returns Err`; bail out.
            if self.inner.rx.changed().await.is_err() {
                return;
            }
        }
    }
}

// -------------------------------------------------------------------------
// Background task
// -------------------------------------------------------------------------

async fn background_refresher(tx: watch::Sender<bool>) {
    let mut ticker = time::interval(Duration::from_secs(30));

    loop {
        ticker.tick().await;
        let ok = dbus_utils::internet_availability();
        let _ = tx.send_if_modified(|old| {
            if *old != ok {
                *old = ok;
                true
            } else {
                false
            }
        });
    }
}
