package main

import (
	"bytes"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"

	"go.uber.org/zap"
)

// GetMacAddress finds the MAC address of the first active, non-loopback network interface.
func GetMacAddress() (string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}

	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp != 0 && iface.Flags&net.FlagLoopback == 0 && iface.HardwareAddr != nil {
			return iface.HardwareAddr.String(), nil
		}
	}
	return "", fmt.Errorf("no suitable network interface found")
}

// GetCpuTemp reads the CPU temperature.
func GetCpuTemp() float64 {
	cmd := exec.Command("sensors", "-u")
	output, err := cmd.Output()
	if err != nil {
		logger.Error("Failed to execute sensors -u", zap.Error(err))
		return 0.0
	}

	lines := bytes.Split(output, []byte("\n"))
	inPkg := false

	for _, line := range lines {
		strLine := strings.TrimSpace(string(line))

		if strLine == "" {
			inPkg = false
			continue
		}

		if strings.HasPrefix(strLine, "Package id 0:") {
			inPkg = true
			continue
		}

		if inPkg && strings.HasPrefix(strLine, "temp1_input:") {
			parts := strings.Fields(strLine)
			if len(parts) == 2 {
				temp, err := strconv.ParseFloat(parts[1], 64)
				if err == nil {
					return temp
				}
			}
		}
	}

	logger.Warn("Warning: Could not find temp1_input for Package id 0")
	return 0.0
}
