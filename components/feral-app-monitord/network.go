package main

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"time"
)

// CheckConnectivity pings a reliable host to verify internet access.
func CheckConnectivity() bool {
	conn, err := net.DialTimeout("tcp", "8.8.8.8:53", 2*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// SendPayload sends the given JSON payload to the specified URL.
func SendPayload(payload []byte) error {
	req, err := http.NewRequest("POST", config.HeartbeatEndpoint, bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("server responded with non-success status: %s", resp.Status)
	}

	return nil
}
