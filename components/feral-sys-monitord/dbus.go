package main

import (
	"github.com/feral-file/godbus"
	"github.com/godbus/dbus/v5"
)

const (
	DBUS_INTERFACE godbus.Interface = "com.feralfile.sysmonitord"
	DBUS_NAME      string           = "com.feralfile.sysmonitord"
	DBUS_PATH      godbus.Path      = "/com/feralfile/sysmonitord"

	DBUS_EVENT_SYSMETRICS          godbus.Member = "sysmetrics"
	DBUS_EVENT_CONNECTIVITY_CHANGE godbus.Member = "connectivity_change"
	DBUS_EVENT_SYSEVENT            godbus.Member = "sysevent"
)

type SysMonitordDBus struct {
	connectivity *Connectivity
}

func NewSysMonitordDBus(connectivity *Connectivity) *SysMonitordDBus {
	return &SysMonitordDBus{
		connectivity: connectivity,
	}
}

func (s *SysMonitordDBus) GetConnectivityStatus(refresh bool) (bool, *dbus.Error) {
	if refresh {
		connected, err := s.connectivity.CheckConnectivity(RPC_PING_TIMEOUT)
		if err != nil {
			return false, dbus.NewError(err.Error(), []interface{}{})
		}
		return connected, nil
	} else {
		return s.connectivity.GetLastConnected(), nil
	}
}
