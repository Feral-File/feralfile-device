mod ble;
mod cache;
mod cdp;
mod constant;
mod dbus_utils;
mod encoding;
mod wifi_utils;

use crate::wifi_utils::SSIDsCacher;
use ble::BLE;
use cache::Cache;
use cdp::CDP;
use std::error::Error;
use std::sync::Arc;
use std::sync::atomic::{AtomicBool, Ordering};
use tokio::signal::unix::{SignalKind, signal as unix_signal};
use tokio::{task, time, time::Duration};

struct AppState {
    device_id: String,
    app_cache: Cache,
    internet: AtomicBool,
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn Error>> {
    // Initialize dependencies
    let chrome = Arc::new(CDP::connect(constant::CDP_URL).await?);
    let ble_service = Arc::new(BLE::new());
    let app_state = Arc::new(AppState {
        device_id: ble_service.get_device_id().await,
        app_cache: Cache::new(constant::CACHE_FILEPATH),
        internet: AtomicBool::new(internet_availability()?),
    });
    // TODO: remove this after testing
    // app_state.internet.store(false, Ordering::Relaxed);

    // Start bluetooth advertising with callbacks
    let connect_wifi_cb = create_wifi_connected_cb(app_state.clone(), chrome.clone());
    let get_info_cb = create_get_info_cb(app_state.clone());
    let ssids_cacher = Arc::new(SSIDsCacher::new());
    match ble_service
        .start(None, connect_wifi_cb, get_info_cb, ssids_cacher.clone())
        .await
    {
        Ok(_) => println!("MAIN: Bluetooth advertising started successfully"),
        Err(e) => {
            println!("MAIN: Error starting Bluetooth advertising: {}", e);
            return Err(e);
        }
    }

    // Startup flow:
    // Show Webapp if we have both cache & internet
    let has_cache = app_state.app_cache.get(cache::TOPIC_ID).is_some();
    let has_internet = app_state.internet.load(Ordering::Acquire);
    if has_cache && has_internet {
        println!("MAIN: has cache & internet, showing webapp");
        show_webapp(&chrome).await?;
    } else {
        // If we don't have either, show the QRCode for user to set up again
        // But in case there is cache without internet,
        // We auto redirect to the webapp if the user fixes the internet
        let auto_redirect = has_cache && !has_internet;
        println!(
            "MAIN: cache = {}, internet = {}, auto_redirect = {}",
            has_cache, has_internet, auto_redirect
        );
        show_qrcode(&app_state, &chrome, auto_redirect).await?;
    }

    // Listen for QRCode switch signal
    let qrcode_switch_cb = create_qrcode_switch_cb(app_state.clone(), chrome.clone());
    let stop_dbus_listener = Arc::new(AtomicBool::new(false));
    dbus_utils::listen_for_signal(
        constant::DBUS_CONNECTD_OBJECT,
        constant::DBUS_CONNECTD_INTERFACE,
        constant::DBUS_EVENT_QRCODE_SWITCH,
        stop_dbus_listener.clone(),
        qrcode_switch_cb,
    );

    // Wait for Ctrl+C or shutdown event
    wait_for_shutdown().await; // Ignore any errors
    println!("MAIN: Shutting down...");
    println!("MAIN: Stopping DBus listener...");
    stop_dbus_listener.store(true, Ordering::Relaxed);
    println!("MAIN: Stopping BLE service...");
    match ble_service.stop().await {
        Ok(_) => println!("MAIN: BLE service stopped"),
        Err(e) => println!("MAIN: Error stopping BLE service: {}", e),
    }
    println!("MAIN: Shutting down...");
    Ok(())
}

fn create_wifi_connected_cb(
    app_state: Arc<AppState>,
    chromium: Arc<CDP>,
) -> ble::ConnectWifiCallback {
    Some(Box::new(move |topic_id: &str| {
        // TODO: we assume that the internet is available when the wifi is connected
        // We should check the internet availability again
        app_state.app_cache.set(cache::TOPIC_ID, topic_id);
        app_state.app_cache.save(constant::CACHE_FILEPATH);
        app_state.internet.store(true, Ordering::Relaxed);
        let chromium = chromium.clone();
        task::spawn(async move {
            time::sleep(Duration::from_millis(constant::NETWORK_CHANGED_DELAY)).await;
            // TODO: using navigate_when_online here is safer
            // As wifi might be available but the internet is not
            // So user can just scan the QRCode again and fix the internet
            // But later, if we can detect the internet availability and deal with it,
            // We can use show_webapp instead to avoid duplicated code
            match chromium.navigate_when_online(constant::WEBAPP_URL).await {
                Ok(_) => println!("MAIN: Navigated to webapp"),
                Err(e) => println!("MAIN: Error navigating to webapp: {}", e),
            };
        });
    }))
}

fn create_get_info_cb(app_state: Arc<AppState>) -> ble::GetInfoCallback {
    Some(Box::new(move || {
        app_state
            .app_cache
            .get(cache::TOPIC_ID)
            .map(|topic_id| vec![topic_id.to_string()])
            .unwrap_or_default()
    }))
}

fn create_qrcode_switch_cb(
    app_state: Arc<AppState>,
    chromium: Arc<CDP>,
) -> dbus_utils::ListenCallback {
    Box::new(move |msg| {
        let chromium = chromium.clone();
        let app_state = app_state.clone();
        let mut qrcode_requested = false;
        match msg.read1::<bool>() {
            Ok(true) => qrcode_requested = true,
            Err(e) => println!("MAIN: Error reading message: {}", e),
            _ => {}
        }
        task::spawn(async move {
            if qrcode_requested {
                let _ = show_qrcode(&app_state, &chromium, false).await;
            } else {
                let _ = show_webapp(&chromium).await;
            }
        });
    })
}

// The url format is like this
// url?step=qr&device_id=<device_id>|<topic_id>|<internet>
fn build_qrcode_url(app_state: &Arc<AppState>) -> String {
    let mut qrcode_url = format!("{}{}", constant::QRCODE_URL_PREFIX, app_state.device_id);
    if app_state.app_cache.get(cache::TOPIC_ID).is_some() {
        qrcode_url = format!(
            "{}|{}",
            qrcode_url,
            app_state.app_cache.get(cache::TOPIC_ID).unwrap()
        );
        let has_internet = app_state.internet.load(Ordering::Relaxed);
        qrcode_url = format!("{}|{}", qrcode_url, {
            if has_internet { "true" } else { "false" }
        });
    }
    qrcode_url
}

async fn wait_for_shutdown() {
    // SIGINT  = Ctrl-C on the terminal
    // SIGTERM = “polite” kill sent by most service managers / docker / k8s
    // (add more signals if you need them)
    let mut sigint = unix_signal(SignalKind::interrupt()).expect("SIGINT handler");
    let mut sigterm = unix_signal(SignalKind::terminate()).expect("SIGTERM handler");

    tokio::select! {
        _ = sigint.recv()  => {},
        _ = sigterm.recv() => {},
    }
}

fn internet_availability() -> Result<bool, Box<dyn Error>> {
    match dbus_utils::call_method(
        constant::DBUS_SYSMONITORD_DESTINATION,
        constant::DBUS_SYSMONITORD_OBJECT,
        constant::DBUS_SYSMONITORD_INTERFACE,
        constant::DBUS_CONNECTIVITY_METHOD,
        true, // payload
        constant::DBUS_INTERNET_CHECK_TIMEOUT,
    ) {
        Ok(response) => {
            let status = response.read1::<bool>().unwrap();
            Ok(status)
        }
        Err(e) => {
            println!("MAIN: Error checking internet availability: {}", e);
            Err(e)
        }
    }
}

async fn show_qrcode(
    app_state: &Arc<AppState>,
    chrome: &Arc<CDP>,
    auto_redirect: bool,
) -> Result<(), Box<dyn Error>> {
    let qrcode_url = build_qrcode_url(&app_state);
    match chrome.navigate(&qrcode_url).await {
        Ok(_) => println!("MAIN: Navigated to {}", qrcode_url),
        Err(e) => {
            println!("MAIN: Error navigating to qrcode: {}", e);
            return Err(e);
        }
    };
    if auto_redirect {
        match chrome.navigate_when_online(constant::WEBAPP_URL).await {
            Ok(_) => println!(
                "MAIN: successfully set up auto redirect to {}",
                constant::WEBAPP_URL
            ),
            Err(e) => {
                println!("MAIN: Error setting up auto redirect: {}", e);
                return Err(e);
            }
        };
    }
    Ok(())
}

async fn show_webapp(chrome: &Arc<CDP>) -> Result<(), Box<dyn Error>> {
    match chrome.navigate(constant::WEBAPP_URL).await {
        Ok(_) => println!("MAIN: Navigated to {}", constant::WEBAPP_URL),
        Err(e) => {
            println!("MAIN: Error navigating to webapp: {}", e);
            return Err(e);
        }
    };
    Ok(())
}
