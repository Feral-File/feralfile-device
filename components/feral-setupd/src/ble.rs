use crate::constant;
use crate::dbus_utils;
use crate::encoding;
use crate::wifi_utils;
use crate::wifi_utils::SSIDsCacher;
use bluer::{
    Adapter, Session,
    adv::Advertisement,
    adv::AdvertisementHandle,
    gatt::local::{
        Application,
        ApplicationHandle,
        Characteristic,
        CharacteristicNotifier,
        // Add notify imports
        CharacteristicNotify,
        CharacteristicNotifyMethod,
        CharacteristicWrite,
        CharacteristicWriteMethod,
        ReqError,
        Service,
    },
};
use futures_util::future::FutureExt;
use std::error::Error;
use std::process::Command;
use std::sync::Arc;
use std::time::Instant;
use tokio::sync::Mutex;
use tokio::task;

const SUCCESS_CODE: &[u8] = &[0];
const ERR_CODE_WRONG_WIFI_PWD: &[u8] = &[1];
const ERR_CODE_UNKNOWN_ERROR: &[u8] = &[255];

pub type BTConnectedCallback = Option<Box<dyn Fn() + Send + Sync>>;
pub type ConnectWifiCallback = Option<Box<dyn Fn(&str) + Send + Sync>>;
pub type GetInfoCallback = Option<Box<dyn Fn() -> Vec<String> + Send + Sync>>;

#[derive(Default)]
struct Inner {
    device_id: String,
    advertised: bool,
    adapter: Option<Adapter>,
    adv_handle: Option<AdvertisementHandle>,
    app_handle: Option<ApplicationHandle>,
}

pub struct BLE {
    inner: Mutex<Inner>,
}

impl BLE {
    pub fn new() -> Self {
        let device_id = encoding::get_device_id();
        Self {
            inner: Mutex::new(Inner {
                device_id,
                ..Default::default()
            }),
        }
    }

    pub async fn start(
        &self,
        bt_connected_cb: BTConnectedCallback,
        connect_wifi_cb: ConnectWifiCallback,
        get_info_cb: GetInfoCallback,
        ssids_cacher: Arc<SSIDsCacher>,
    ) -> Result<(), Box<dyn Error>> {
        let mut inner = self.inner.lock().await;
        if inner.advertised {
            return Ok(());
        }

        // Initialize BlueZ session and power on adapter
        let session = Session::new().await?;
        let adapter = session.default_adapter().await?;
        adapter.set_powered(true).await?;
        println!(
            "BLE: Adapter {} powered on for {}",
            adapter.name(),
            inner.device_id
        );

        // Start advertising our service UUID
        let adv = Advertisement {
            service_uuids: vec![constant::SERVICE_UUID].into_iter().collect(),
            discoverable: Some(true),
            local_name: Some(inner.device_id.clone()),
            ..Default::default()
        };
        let adv_handle = adapter.advertise(adv).await?;
        println!("BLE: Advertising GATT service {}", constant::SERVICE_UUID);

        // Group into a GATT service and register it
        let svc = Service {
            uuid: constant::SERVICE_UUID,
            primary: true,
            characteristics: vec![
                self.create_cmd_char(bt_connected_cb, connect_wifi_cb, get_info_cb, ssids_cacher)
                    .await,
            ],
            ..Default::default()
        };
        let app = Application {
            services: vec![svc],
            ..Default::default()
        };
        let app_handle = adapter.serve_gatt_application(app).await?;
        println!("BLE: GATT app registered; ready to receive commands");

        inner.adapter = Some(adapter);
        inner.adv_handle = Some(adv_handle);
        inner.app_handle = Some(app_handle);
        inner.advertised = true;
        Ok(())
    }

    pub async fn stop(&self) -> Result<(), Box<dyn Error>> {
        let (adv, app, adapter) = {
            let mut inner = self.inner.lock().await;
            if !inner.advertised {
                return Ok(());
            }
            inner.advertised = false;
            (
                inner.adv_handle.take(),
                inner.app_handle.take(),
                inner.adapter.take(),
            )
        };
        // Disconnect all devices (to make sure BlueZ doesn't block adv unregistration)
        if let Some(adapter) = adapter {
            for addr in adapter.device_addresses().await? {
                println!("BLE: Disconnecting device {:?}", addr);
                let dev = adapter.device(addr)?;
                println!(
                    "BLE: Device {:?} is connected: {:?}",
                    addr,
                    dev.is_connected().await?
                );
                if dev.is_connected().await? {
                    let _ = dev.disconnect().await; // ignore errors
                }
            }
            drop(adv);
            drop(app);
            tokio::time::sleep(std::time::Duration::from_millis(
                constant::BLE_SHUTDOWN_DELAY,
            ))
            .await;
        }
        Ok(())
    }

    pub async fn get_device_id(&self) -> String {
        let st = self.inner.lock().await;
        st.device_id.clone()
    }

    async fn create_cmd_char(
        &self,
        bt_connected_cb: BTConnectedCallback,
        connect_wifi_cb: ConnectWifiCallback,
        get_info_cb: GetInfoCallback,
        ssids_cacher: Arc<SSIDsCacher>,
    ) -> Characteristic {
        // Shared storage for the notifier handle
        let notifier: Arc<Mutex<Option<CharacteristicNotifier>>> = Arc::new(Mutex::new(None));
        let notifier_for_write = notifier.clone();
        let notifier_for_notify = notifier.clone();

        let bt_connected_callback = Arc::new(bt_connected_cb);
        let connect_wifi_callback = Arc::new(connect_wifi_cb);
        let get_info_callback = Arc::new(get_info_cb);
        Characteristic {
            uuid: constant::CMD_CHAR_UUID,
            // Enable notifications on this characteristic
            notify: Some(CharacteristicNotify {
                notify: true,
                method: CharacteristicNotifyMethod::Fun(Box::new(move |notifier| {
                    println!("BLE: a device is connected");
                    let handle = notifier_for_notify.clone();
                    let bt_connected_callback = bt_connected_callback.clone();
                    async move {
                        // Store the notifier for later use in the write callback
                        *handle.lock().await = Some(notifier);
                        if let Some(cb) = bt_connected_callback.as_ref() {
                            cb();
                        }
                    }
                    .boxed()
                })),
                ..Default::default()
            }),
            write: Some(CharacteristicWrite {
                write: true,
                write_without_response: false,
                method: CharacteristicWriteMethod::Fun(Box::new(move |data, _req| {
                    println!("BLE: Received bluetooth data {:?}", data);
                    let notifier = notifier_for_write.clone();
                    let connect_wifi_callback = connect_wifi_callback.clone();
                    let get_info_callback = get_info_callback.clone();
                    let ssids_cacher = ssids_cacher.clone();
                    async move {
                        let payload = encoding::parse_payload(&data);
                        // No values, or malformed payload
                        if payload.is_none() {
                            eprintln!("BLE: Received malformed payload");
                            return Ok::<(), ReqError>(());
                        }
                        let vals = payload.unwrap();
                        // Not enough values, or malformed payload
                        if vals.len() < 2 {
                            eprintln!("BLE: Received payload with only {} values", vals.len());
                            return Ok::<(), ReqError>(());
                        }
                        // Enough values, parse command
                        println!("BLE: Payload: {:?}", vals);
                        let cmd = vals[0].clone();
                        let reply_id = vals[1].clone();
                        let params = vals[2..].to_vec();
                        match cmd.as_str() {
                            constant::CMD_SCAN_WIFI => {
                                handle_scan_wifi(notifier, reply_id, ssids_cacher).await
                            }
                            constant::CMD_CONNECT_WIFI => {
                                handle_connect_wifi(
                                    notifier,
                                    reply_id,
                                    params,
                                    connect_wifi_callback,
                                )
                                .await
                            }
                            constant::CMD_GET_INFO => {
                                handle_get_info(notifier, reply_id, get_info_callback).await
                            }
                            constant::CMD_SET_TIME => {
                                handle_set_time(notifier, reply_id, params).await
                            }
                            _ => {
                                eprintln!("BLE: Unknown command: {}", cmd);
                                Ok::<(), ReqError>(())
                            }
                        }
                    }
                    .boxed()
                })),
                ..Default::default()
            }),
            ..Default::default()
        }
    }
}

async fn handle_scan_wifi(
    notifier: Arc<Mutex<Option<CharacteristicNotifier>>>,
    reply_id: String,
    ssids_cacher: Arc<SSIDsCacher>,
) -> Result<(), ReqError> {
    // Scan available SSIDs using the helper
    let start_time = Instant::now();
    let ssids = match ssids_cacher.get().await {
        Ok(v) => v,
        Err(e) => {
            eprintln!("BLE: Failed to scan wifi: {}", e);
            return Ok(());
        }
    };
    println!(
        "BLE: Found SSIDs \n{:?} in {:?} ms",
        ssids,
        start_time.elapsed().as_millis()
    );

    // Build BLE reply payload
    let mut reply = Vec::with_capacity(ssids.len() + 2);
    reply.push(reply_id.as_bytes());
    reply.push(SUCCESS_CODE);
    reply.extend(ssids.iter().map(|s| s.as_bytes()));
    println!("BLE: Reply: {:?}", reply);
    let payload = encoding::encode_payload(&reply);

    // Notify the central (if notifier is already registered)
    let mut guard = notifier.lock().await;
    if let Some(notifier) = guard.as_mut() {
        match notifier.notify(payload).await {
            Ok(_) => (),
            Err(e) => {
                eprintln!("BLE: Failed to notify central after scanning wifi: {}", e);
            }
        }
    } else {
        eprintln!("BLE: Notifier not yet available; skipping reply");
    }
    Ok(())
}

async fn handle_connect_wifi(
    notifier: Arc<Mutex<Option<CharacteristicNotifier>>>,
    reply_id: String,
    params: Vec<String>,
    cb: Arc<ConnectWifiCallback>,
) -> Result<(), ReqError> {
    // Expect at least SSID and password
    if params.len() < 2 {
        eprintln!(
            "BLE: Received wifi payload with only {} values",
            params.len()
        );
        return Ok(());
    }

    let ssid = &params[0];
    let pass = &params[1];

    // Attempt connection
    // If it fails, notify central with error code
    let start_time = Instant::now();
    let topic_id: String;
    let mut payload = Vec::with_capacity(3); // to reply to central
    payload.push(reply_id.as_bytes());
    if let Err(e) = wifi_utils::connect(ssid, pass) {
        eprintln!(
            "BLE: Failed to connect to wifi \"{}\" in {:?} ms: {}",
            ssid,
            start_time.elapsed().as_millis(),
            e
        );
        // This is a bit of a hack to detect wrong password
        // But the command doesn't provide a reliable way to detect this
        let error_code = if e.to_string().contains("password") {
            ERR_CODE_WRONG_WIFI_PWD
        } else {
            ERR_CODE_UNKNOWN_ERROR
        };
        payload.push(error_code);
    } else {
        println!(
            "BLE: Connected to wifi \"{}\" in {:?} ms",
            ssid,
            start_time.elapsed().as_millis()
        );
        topic_id = match get_relayer_info().await {
            Ok(info) => info,
            Err(e) => {
                eprintln!("BLE: can't get relayer data from connectd: {}", e);
                return Ok(());
            }
        };
        println!("BLE: Relay‑server topic: {}", topic_id);

        if let Some(cb) = cb.as_ref() {
            cb(&topic_id);
        }

        payload.push(SUCCESS_CODE);
        payload.push(topic_id.as_bytes());
    }

    let mut guard = notifier.lock().await;
    if let Some(notifier) = guard.as_mut() {
        println!("BLE: Reply: {:?}", payload);
        let payload = encoding::encode_payload(&payload);
        match notifier.notify(payload).await {
            Ok(_) => (),
            Err(e) => {
                eprintln!("BLE: Failed to send relayer info: {}", e);
            }
        }
    } else {
        eprintln!("BLE: Notifier not yet available; skipping reply");
    }

    Ok(())
}

async fn handle_get_info(
    notifier: Arc<Mutex<Option<CharacteristicNotifier>>>,
    reply_id: String,
    cb: Arc<GetInfoCallback>,
) -> Result<(), ReqError> {
    let payload = if let Some(cb) = cb.as_ref() {
        cb()
    } else {
        vec![]
    };
    let mut reply = Vec::with_capacity(payload.len() + 1);
    reply.push(reply_id.as_bytes());
    reply.push(SUCCESS_CODE);
    reply.extend(payload.iter().map(|s| s.as_bytes()));

    let mut guard = notifier.lock().await;
    if let Some(notifier) = guard.as_mut() {
        let payload = encoding::encode_payload(&reply);
        match notifier.notify(payload).await {
            Ok(_) => (),
            Err(e) => {
                eprintln!("BLE: Failed to notify central after getting info: {}", e);
            }
        }
    } else {
        eprintln!("BLE: Notifier not yet available; skipping reply");
    }
    Ok(())
}

async fn handle_set_time(
    _notifier: Arc<Mutex<Option<CharacteristicNotifier>>>,
    _reply_id: String,
    params: Vec<String>,
) -> Result<(), ReqError> {
    println!("BLE: Setting time");
    if params.len() < 2 {
        eprintln!(
            "BLE: Received timezone payload with only {} values",
            params.len()
        );
        return Ok(());
    }
    match task::spawn_blocking(move || {
        let timezone = &params[0];
        let time = &params[1];
        let result = Command::new(constant::TIMEZONE_CMD)
            .args(&[constant::TIMEZONE_INSTRUCTION, timezone, time])
            .output();
        println!("BLE: Result: {:?}", result);
        if result.is_ok() {
            println!("BLE: Time set successfully");
            Ok::<(), Box<dyn Error + Send + Sync>>(())
        } else {
            println!("BLE: Failed to set time");
            Err("Failed to set time".into())
        }
    })
    .await
    {
        Err(e) => {
            eprintln!("BLE: Failed to start time setting thread: {}", e);
        }
        _ => (),
    };
    Ok(())
}

async fn get_relayer_info() -> Result<String, Box<dyn Error + Send + Sync>> {
    let start_time = Instant::now();

    // Start listening **before** we announce the Wi‑Fi connection so we don't
    // miss the very first `relayer_configured` signal sent by `connectd`.
    println!("BLE: Preparing to wait for relayer topic");
    let recv_task = task::spawn_blocking(|| {
        dbus_utils::receive(
            constant::DBUS_CONNECTD_OBJECT,
            constant::DBUS_CONNECTD_INTERFACE,
            constant::DBUS_EVENT_RELAYER_CONFIGURED,
            constant::DBUS_CONNECTD_TIMEOUT,
        )
    });

    // Now emit the `wifi_connected` event (this waits for its own ack).
    println!("BLE: Sending wifi_connected event");
    task::spawn_blocking(|| {
        dbus_utils::send(
            constant::DBUS_SETUPD_OBJECT,
            constant::DBUS_SETUPD_INTERFACE,
            constant::DBUS_EVENT_WIFI_CONNECTED,
            "", // empty payload
        )
    })
    .await??;

    // Await the relayer information we were already listening for.
    let msg = recv_task.await??;
    let topic_id = msg.read1::<String>()?;
    println!(
        "BLE: Relayer info received in {:?} ms",
        start_time.elapsed().as_millis()
    );
    Ok(topic_id)
}
