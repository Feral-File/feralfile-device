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
use std::time::Instant;
use tokio::signal::unix::{SignalKind, signal as unix_signal};
use tokio::{
    sync::Mutex,
    task,
    time::{self, Duration},
};

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum Page {
    None,
    QRCode,
    WebApp,
}

#[derive(Debug)]
struct AppState {
    device_id: String,
    app_cache: Cache,
    internet: AtomicBool,
    page: Mutex<Page>,
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn Error + Send + Sync>> {
    // Initialize dependencies
    let chrome = match CDP::connect(constant::CDP_URL).await {
        Ok(chrome) => chrome,
        Err(e) => {
            eprintln!("MAIN: Error connecting to CDP: {}", e);
            return Err(format!("Error connecting to CDP: {}", e).into());
        }
    };
    let chrome = Arc::new(chrome);
    let ble_service = Arc::new(BLE::new());
    let app_state = Arc::new(AppState {
        device_id: ble_service.get_device_id().await,
        app_cache: Cache::new(constant::CACHE_FILEPATH),
        internet: AtomicBool::new(dbus_utils::internet_availability()),
        page: Mutex::new(Page::None),
    });
    println!("MAIN: App state initialized: {:?}", app_state);

    // Start bluetooth advertising with callbacks
    let bt_connected_cb = create_bt_connected_cb(chrome.clone());
    let connect_wifi_cb = create_connect_wifi_cb(app_state.clone(), chrome.clone());
    let get_info_cb = create_get_info_cb(app_state.clone());
    let ssids_cacher = Arc::new(SSIDsCacher::new());
    match ble_service
        .start(
            bt_connected_cb,
            connect_wifi_cb,
            get_info_cb,
            ssids_cacher.clone(),
        )
        .await
    {
        Ok(_) => println!("MAIN: Bluetooth advertising started successfully"),
        Err(e) => {
            eprintln!("MAIN: Error starting Bluetooth advertising: {}", e);
            return Err(format!("Error starting Bluetooth advertising: {}", e).into());
        }
    }

    let has_internet = app_state.internet.load(Ordering::Relaxed);
    let has_cache = app_state.app_cache.get(cache::TOPIC_ID).is_some();
    if !has_cache {
        // First time using the app, just show the QRCode
        ssids_cacher.trigger_refresh();
        let _ = show_qrcode(&app_state, &chrome, false).await;
    } else {
        // Second time using the app
        if has_internet {
            // All good, show webapp
            let _ = show_webapp(&app_state, &chrome).await;
        } else {
            // No internet, show QRCode and wait for user to fix it
            ssids_cacher.trigger_refresh();
            let _ = show_qrcode(&app_state, &chrome, true).await;
        }
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

fn create_bt_connected_cb(chromium: Arc<CDP>) -> ble::BTConnectedCallback {
    Some(Box::new(move || {
        let chromium = chromium.clone();
        Box::pin(async move {
            let _ = show_message(&chromium, constant::WELCOME_MSG).await;
        })
    }))
}

fn create_connect_wifi_cb(
    app_state: Arc<AppState>,
    chromium: Arc<CDP>,
) -> ble::ConnectWifiCallback {
    Box::new(move |ssid, pwd| {
        let app_state = app_state.clone();
        let chromium = chromium.clone();
        let ssid = ssid.to_string();
        let pwd = pwd.to_string();
        Box::pin(async move {
            let start_time = Instant::now();
            // Show message
            let _ = show_message(
                &chromium,
                &format!("{}{}", constant::WIFI_CONNECTING_MSG_PREFIX, ssid),
            )
            .await;

            // Connect to wifi & return early if failed
            if let Err(e) = wifi_utils::connect(&ssid, &pwd) {
                eprintln!(
                    "MAIN: Failed to connect to wifi \"{}\" in {:?} ms: {}",
                    ssid,
                    start_time.elapsed().as_millis(),
                    e
                );
                task::spawn(async move {
                    let _ = show_message(&chromium, constant::WIFI_FAILED_TO_CONNECT_MSG).await;
                });
                // This is a bit of a hack to detect wrong password
                // But the command doesn't provide a reliable way to detect this
                return if e.to_string().contains("password") {
                    Err(constant::BLE_ERR_CODE_WRONG_WIFI_PWD)
                } else {
                    Err(constant::BLE_ERR_CODE_UNKNOWN_ERROR)
                };
            }

            // Return early if there is no internet
            let internet = dbus_utils::internet_availability();
            if !internet {
                task::spawn(async move {
                    let _ = show_message(&chromium, constant::INTERNET_FAILED_TO_CONNECT_MSG).await;
                });
                return Err(constant::BLE_ERR_CODE_NO_INTERNET);
            }

            // Get topic id from connectd
            let topic_id = match dbus_utils::get_relayer_info() {
                Ok(info) => info,
                Err(e) => {
                    eprintln!("BLE: can't get relayer data from connectd: {}", e);
                    return Err(constant::BLE_ERR_CODE_UNKNOWN_ERROR);
                }
            };

            app_state.app_cache.set(cache::TOPIC_ID, &topic_id);
            app_state.app_cache.save(constant::CACHE_FILEPATH);
            app_state.internet.store(true, Ordering::Relaxed);
            task::spawn(async move {
                // This is a workaround to avoid Err Network Changed from Chrome
                // This potentially also avoids the white screen issue
                let _ = show_message(&chromium, constant::SETUP_SUCCESSFULLY_MSG).await;
                time::sleep(Duration::from_millis(constant::WIFI_WEBAPP_DELAY)).await;
                let _ = show_webapp(&app_state, &chromium).await;
            });
            Ok(topic_id)
        })
    })
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
                let _ = show_webapp(&app_state, &chromium).await;
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

async fn show_qrcode(
    app_state: &Arc<AppState>,
    chrome: &Arc<CDP>,
    redirect_when_online: bool,
) -> Result<(), Box<dyn Error + Send + Sync>> {
    let qrcode_url = build_qrcode_url(&app_state);
    // QRCode url is dynamically built
    // So we always navigate to make sure the url is correct
    let mut page = app_state.page.lock().await;
    match chrome.navigate(&qrcode_url).await {
        Ok(_) => {
            println!("MAIN: Navigated to {}", qrcode_url);
            *page = Page::QRCode;
        }
        Err(e) => {
            println!("MAIN: Error navigating to qrcode: {}", e);
            return Err(e);
        }
    };
    if redirect_when_online {
        let stop_listening = Arc::new(AtomicBool::new(false));
        let app_state = app_state.clone();
        let chrome = chrome.clone();
        dbus_utils::on_internet_available(
            move || {
                let app_state = app_state.clone();
                let chrome = chrome.clone();
                task::spawn(async move {
                    let _ = show_webapp(&app_state, &chrome).await;
                });
            },
            stop_listening,
        );
    }
    Ok(())
}

async fn show_webapp(
    app_state: &Arc<AppState>,
    chrome: &Arc<CDP>,
) -> Result<(), Box<dyn Error + Send + Sync>> {
    let mut page = app_state.page.lock().await;
    // For webapp, we only navigate if the page is not it already
    if *page == Page::WebApp {
        return Ok(());
    }

    // This is to avoid Err Network Changed from Chrome
    time::sleep(Duration::from_millis(constant::WIFI_WEBAPP_DELAY)).await;

    match chrome.navigate(constant::WEBAPP_URL).await {
        Ok(_) => {
            println!("MAIN: Navigated to {}", constant::WEBAPP_URL);
            *page = Page::WebApp;
        }
        Err(e) => {
            println!("MAIN: Error navigating to webapp: {}", e);
            return Err(e);
        }
    };
    Ok(())
}

async fn show_message(
    chrome: &Arc<CDP>,
    message: &str,
) -> Result<(), Box<dyn Error + Send + Sync>> {
    let message_url = format!("{}{}", constant::MSG_URL_PREFIX, message);
    match chrome.navigate(&message_url).await {
        Ok(_) => {
            println!("MAIN: Navigated to {}", message_url);
            Ok(())
        }
        Err(e) => {
            println!("MAIN: Error showing message: {}", e);
            Err(e)
        }
    }
}
