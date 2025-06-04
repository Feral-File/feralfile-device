package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
)

// DeviceStatusResponse represents the structure of device status information
type DeviceStatusResponse struct {
	ScreenRotation   string `json:"screenRotation,omitempty"`
	ConnectedWifi    string `json:"connectedWifi,omitempty"`
	InstalledVersion string `json:"installedVersion,omitempty"`
	LatestVersion    string `json:"latestVersion,omitempty"`
}

// GetDeviceStatus retrieves comprehensive device status information
// This function can be used by both command handlers and status polling
func GetDeviceStatus(ctx context.Context) (*DeviceStatusResponse, error) {
	response := &DeviceStatusResponse{}

	// Use errgroup for parallel execution
	g, ctx := errgroup.WithContext(ctx)

	// Variables to collect results safely
	var screenRotation, connectedWifi, installedVersion, latestVersion string

	// Get screen rotation
	g.Go(func() error {
		// Default to landscape
		screenRotation = "landscape"

		configPath := "/home/feralfile/.config/screen-orientation"
		configData, err := os.ReadFile(configPath)
		if err != nil {
			return nil // Don't fail if config file doesn't exist
		}

		if len(configData) > 0 {
			savedRotation := strings.TrimSpace(string(configData))
			orientationMap := map[string]string{
				"normal": "landscape",
				"90":     "portrait",
				"180":    "landscapeReverse",
				"270":    "portraitReverse",
			}
			if orientation, ok := orientationMap[savedRotation]; ok {
				screenRotation = orientation
			}
		}
		return nil
	})

	// Get WiFi information
	g.Go(func() error {
		cmd := exec.CommandContext(ctx, "nmcli", "-t", "-f", "NAME,DEVICE,STATE", "connection", "show", "--active")
		output, err := cmd.Output()
		if err != nil {
			return nil // Don't fail if nmcli command fails
		}

		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			parts := strings.Split(line, ":")
			if len(parts) >= 3 && parts[2] == "activated" {
				// Check if device name starts with 'wl' (wireless) or contains 'wifi'
				deviceName := parts[1]
				if strings.HasPrefix(deviceName, "wl") || strings.Contains(deviceName, "wifi") {
					connectedWifi = parts[0] // Network name
					break
				}
			}
		}
		return nil
	})

	// Get installed version and latest version
	g.Go(func() error {
		configFile := "/home/feralfile/x1-config.json"
		configBytes, err := os.ReadFile(configFile)
		if err != nil {
			return fmt.Errorf("failed to read config file: %w", err)
		}

		var config struct {
			Version          string `json:"version"`
			Branch           string `json:"branch"`
			DistributionAcc  string `json:"distribution_acc"`
			DistributionPass string `json:"distribution_pass"`
		}

		if err := json.Unmarshal(configBytes, &config); err != nil {
			return fmt.Errorf("failed to parse config file: %w", err)
		}

		installedVersion = config.Version

		// Get latest version from API if credentials are available
		if config.Branch != "" && config.DistributionAcc != "" && config.DistributionPass != "" {
			version, err := fetchLatestVersion(ctx, config.Branch, config.DistributionAcc, config.DistributionPass)
			if err != nil {
				return fmt.Errorf("failed to fetch latest version: %w", err)
			}
			latestVersion = version
		}

		return nil
	})

	// Wait for all goroutines to complete
	if err := g.Wait(); err != nil {
		return nil, err
	}

	// Safely assign results after all goroutines complete
	response.ScreenRotation = screenRotation
	response.ConnectedWifi = connectedWifi
	response.InstalledVersion = installedVersion
	response.LatestVersion = latestVersion

	return response, nil
}

// fetchLatestVersion retrieves the latest version from the distribution API
func fetchLatestVersion(ctx context.Context, branch, account, pass string) (string, error) {
	otaAPI := "https://feralfile-device-distribution.bitmark-development.workers.dev/api/latest"
	apiURL := fmt.Sprintf("%s/%s", otaAPI, branch)

	// Create HTTP client with 2-second timeout
	client := &http.Client{
		Timeout: 2 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", err
	}

	// Set basic auth
	req.SetBasicAuth(account, pass)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var apiResponse struct {
		LatestVersion string `json:"latest_version"`
	}

	if err := json.Unmarshal(body, &apiResponse); err != nil {
		return "", err
	}

	return apiResponse.LatestVersion, nil
}
