package main

import (
	"github.com/feral-file/godbus"
)

const (
	DBUS_INTERFACE godbus.Interface = "com.feralfile.connectd.general"
	DBUS_PATH      godbus.Path      = "/com/feralfile/connectd"
	DBUS_NAME      string           = "com.feralfile.connectd"

	DBUS_SETUPD_EVENT_WIFI_CONNECTED            godbus.Member = "wifi_connected"
	DBUS_SETUPD_EVENT_SHOW_PAIRING_QR_CODE      godbus.Member = "show_pairing_qr_code"
	DBUS_SETUPD_EVENT_RELAYER_CONFIGURED        godbus.Member = "relayer_configured"
	DBUS_SYS_MONITORD_EVENT_SYSMETRICS          godbus.Member = "sysmetrics"
	DBUS_SYS_MONITORD_EVENT_CONNECTIVITY_CHANGE godbus.Member = "connectivity_change"
)
