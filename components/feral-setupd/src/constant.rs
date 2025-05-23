use uuid::Uuid;

// Functional configuration
pub const CACHE_FILEPATH: &str = "/home/feralfile/.state/setupd";
pub const TIMEZONE_CMD: &str = "/home/feralfile/scripts/feral-timesyncd.sh";
pub const TIMEZONE_INSTRUCTION: &str = "set-time";
pub const SSID_CACHE_TTL: u64 = 5 * 60 * 1000; // 5 minutes
pub const BLE_SHUTDOWN_DELAY: u64 = 1000; // 1 second
pub const NETWORK_CHANGED_DELAY: u64 = 1; // 1 millisecond

// Bluetooth configuration
pub const SERVICE_UUID: Uuid = Uuid::from_u128(0xf7826da64fa24e988024bc5b71e0893e_u128);
pub const CMD_CHAR_UUID: Uuid = Uuid::from_u128(0x6e400002b5a3f393e0a9e50e24dcca9e_u128);
pub const CMD_CONNECT_WIFI: &str = "connect_wifi";
pub const CMD_SCAN_WIFI: &str = "scan_wifi";
pub const CMD_GET_INFO: &str = "get_info";
pub const CMD_SET_TIME: &str = "set_time";
pub const MAX_SSIDS: usize = 9;
pub const MD5_LENGTH: usize = 8; // Used for conversion to device ID
pub const DEVICE_ID_PREFIX: &str = "FF-X1-";

// Chrome configuration
pub const CDP_URL: &str = "http://127.0.0.1:9222/json";
pub const CDP_ID_START: u64 = 1_000_000;
pub const WEBAPP_URL: &str =
    "https://feat-handle-cdp-request.feralfile-display-prod.pages.dev?platform=ff-device";
pub const QRCODE_URL_PREFIX: &str = "file:///opt/feral/ui/launcher/index.html?step=qr&device_id=";

// D-Bus configuration
pub const DBUS_SETUPD_OBJECT: &str = "/com/feralfile/setupd";
pub const DBUS_CONNECTD_OBJECT: &str = "/com/feralfile/connectd";
pub const DBUS_SYSMONITORD_OBJECT: &str = "/com/feralfile/sysmonitord";
pub const DBUS_SYSMONITORD_DESTINATION: &str = "com.feralfile.sysmonitord";

pub const DBUS_SETUPD_INTERFACE: &str = "com.feralfile.setupd.general";
pub const DBUS_CONNECTD_INTERFACE: &str = "com.feralfile.connectd.general";
pub const DBUS_SYSMONITORD_INTERFACE: &str = "com.feralfile.sysmonitord";

pub const DBUS_EVENT_WIFI_CONNECTED: &str = "wifi_connected";
pub const DBUS_EVENT_RELAYER_CONFIGURED: &str = "relayer_configured";
pub const DBUS_EVENT_QRCODE_SWITCH: &str = "show_pairing_qr_code";
pub const DBUS_CONNECTIVITY_METHOD: &str = "GetConnectivityStatus";

pub const DBUS_CONNECTD_TIMEOUT: u64 = 30 * 1000; // 30 seconds
pub const DBUS_MAX_RETRIES: usize = 6;
pub const DBUS_ACK_TIMEOUT: u64 = 5 * 1000; // 5 seconds
pub const DBUS_INTERNET_CHECK_TIMEOUT: u64 = 5 * 1000; // 5 seconds
pub const DBUS_LISTEN_WAKE_UP_INTERVAL: u64 = 1000; // 1 second
