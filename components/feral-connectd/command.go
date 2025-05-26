package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/feral-file/godbus"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

var CmdOK = struct {
	OK bool `json:"ok"`
}{
	OK: true,
}

type Command struct {
	Command   RelayerCmd
	Arguments map[string]interface{}
}

type Device struct {
	ID       string `json:"device_id"`
	Name     string `json:"device_name"`
	Platform int    `json:"platform"`
}

type CommandHandler struct {
	sync.Mutex
	cdp    *CDPClient
	dbus   *godbus.DBusClient
	logger *zap.Logger

	lastSysMetrics []byte

	// Mouse position tracking
	cursorPositionX   float64
	cursorPositionY   float64
	screenWidth       float64
	screenHeight      float64
	screenInitialized bool
	movingScaleFactor float64
}

func NewCommandHandler(cdp *CDPClient, dbus *godbus.DBusClient, logger *zap.Logger) *CommandHandler {
	return &CommandHandler{
		cdp:    cdp,
		dbus:   dbus,
		logger: logger,
	}
}

func (c *CommandHandler) saveLastSysMetrics(metrics []byte) {
	c.Lock()
	defer c.Unlock()
	c.lastSysMetrics = metrics
}

func (c *CommandHandler) Execute(ctx context.Context, cmd Command) (interface{}, error) {
	c.logger.Info("Executing command", zap.String("command", string(cmd.Command)))

	var err error
	var bytes []byte

	bytes, err = json.Marshal(cmd.Arguments)
	if err != nil {
		return nil, fmt.Errorf("invalid arguments: %s", err)
	}

	var result interface{}
	switch cmd.Command {
	case RELAYER_CMD_CONNECT:
		result, err = c.connect(bytes)
	case RELAYER_CMD_SHOW_PAIRING_QR_CODE:
		result, err = c.showPairingQRCode(ctx, bytes)
	case RELAYER_CMD_KEYBOARD_EVENT:
		result, err = c.handleKeyboardEvent(ctx, bytes)
	case RELAYER_CMD_MOUSE_DRAG_EVENT:
		result, err = c.handleMouseMoveEvent(ctx, bytes)
	case RELAYER_CMD_MOUSE_TAP_EVENT:
		result, err = c.handleMouseTapEvent(ctx, bytes)
	case RELAYER_CMD_SYS_METRICS:
		c.Lock()
		defer c.Unlock()
		var sysMetrics map[string]interface{}
		if c.lastSysMetrics != nil {
			err = json.Unmarshal(c.lastSysMetrics, &sysMetrics)
			if err != nil {
				return nil, fmt.Errorf("failed to unmarshal last sys metrics: %s", err)
			}
		}
		return sysMetrics, nil
	case RELAYER_CMD_SCREEN_ROTATION:
		result, err = c.handleScreenRotation(ctx, bytes)
		return c.lastSysMetrics, nil
	case RELAYER_CMD_SHUTDOWN:
		result, err = c.shutdown(ctx)
	case RELAYER_CMD_DEVICE_STATUS:
		result, err = c.deviceStatus(ctx, bytes)
	default:
		return nil, fmt.Errorf("invalid command: %s", cmd)
	}

	return result, err
}

func (c *CommandHandler) connect(args []byte) (interface{}, error) {
	var cmdArgs struct {
		Device         Device `json:"clientDevice"`
		PrimaryAddress string `json:"primaryAddress"`
	}
	err := json.Unmarshal(args, &cmdArgs)
	if err != nil {
		return nil, fmt.Errorf("invalid arguments: %s", err)
	}

	state := GetState()
	state.ConnectedDevice = &cmdArgs.Device
	err = state.Save()
	if err != nil {
		return nil, fmt.Errorf("failed to save state: %s", err)
	}

	return CmdOK, nil
}

func (c *CommandHandler) showPairingQRCode(ctx context.Context, args []byte) (interface{}, error) {
	var cmdArgs struct {
		Show bool `json:"show"`
	}
	err := json.Unmarshal(args, &cmdArgs)
	if err != nil {
		return nil, fmt.Errorf("invalid arguments: %s", err)
	}

	err = c.dbus.RetryableSend(ctx,
		godbus.DBusPayload{
			Interface: DBUS_INTERFACE,
			Path:      DBUS_PATH,
			Member:    DBUS_SETUPD_EVENT_SHOW_PAIRING_QR_CODE,
			Body:      []interface{}{cmdArgs.Show},
		})
	return CmdOK, nil
}

func (c *CommandHandler) deviceStatus(ctx context.Context, args []byte) (interface{}, error) {
	// Create response structure
	response := struct {
		ScreenRotation   string `json:"screenRotation,omitempty"`
		ConnectedWifi    string `json:"connectedWifi,omitempty"`
		InstalledVersion string `json:"installedVersion,omitempty"`
		LatestVersion    string `json:"latestVersion,omitempty"`
	}{}

	// Use errgroup for parallel execution
	g, ctx := errgroup.WithContext(ctx)

	// Channels to collect results safely
	var screenRotation, connectedWifi, installedVersion, latestVersion string

	// Get screen rotation
	g.Go(func() error {
		// Default to landscape
		screenRotation = "landscape"

		configPath := "/home/feralfile/.config/screen-orientation"
		configData, err := os.ReadFile(configPath)
		if err != nil {
			return nil
		}

		if len(configData) > 0 {
			savedRotation := strings.TrimSpace(string(configData))
			orientationMap := map[string]string{
				"normal": "landscape",
				"90":     "portraitReverse",
				"180":    "landscapeReverse",
				"270":    "portrait",
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
			// Nested function to fetch latest version
			fetchLatestVersion := func(branch, account, pass string) (string, error) {
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

			version, err := fetchLatestVersion(config.Branch, config.DistributionAcc, config.DistributionPass)
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

func (c *CommandHandler) handleScreenRotation(ctx context.Context, args []byte) (interface{}, error) {
	var cmdArgs struct {
		Clockwise bool `json:"clockwise"`
	}

	err := json.Unmarshal(args, &cmdArgs)
	if err != nil {
		return nil, fmt.Errorf("invalid arguments: %s", err)
	}

	clockwise := cmdArgs.Clockwise
	c.logger.Info("Screen rotation request",
		zap.Bool("clockwise", clockwise))

	// Execute wlr-randr command
	cmd := exec.CommandContext(ctx, "wlr-randr")

	// Get current outputs
	output, err := cmd.Output()
	if err != nil {
		c.logger.Error("Failed to execute wlr-randr", zap.Error(err))
		return nil, fmt.Errorf("failed to get display info: %s", err)
	}

	// Find the active output name
	outputName := ""
	lines := strings.Split(string(output), "\n")
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "Output") {
			parts := strings.Split(line, " ")
			if len(parts) > 1 {
				outputName = parts[1]
				break
			}
		} else if i == 0 && len(line) > 0 {
			// First line might directly contain the output name
			parts := strings.Split(line, " ")
			if len(parts) > 0 {
				outputName = parts[0]
				break
			}
		}
	}

	if outputName == "" {
		return nil, fmt.Errorf("could not find active output")
	}

	// Determine rotation
	// Assume normal is 0, then 90, 180, 270 degrees
	rotations := []string{"normal", "90", "180", "270"}

	// Read current orientation from config file (this is what user perceives)
	currentIndex := 0 // Default to normal
	configPath := "/home/feralfile/.config/screen-orientation"
	configData, err := os.ReadFile(configPath)
	if err == nil && len(configData) > 0 {
		savedRotation := strings.TrimSpace(string(configData))
		for i, rot := range rotations {
			if rot == savedRotation {
				currentIndex = i
				break
			}
		}
		c.logger.Info("Using perceived rotation from config", zap.String("rotation", savedRotation))
	} else {
		c.logger.Warn("No saved rotation found, assuming normal orientation")
	}

	// Calculate new orientation based on perceived current orientation
	var newIndex int
	if clockwise {
		newIndex = (currentIndex - 1 + 4) % 4
	} else {
		newIndex = (currentIndex + 1) % 4
	}

	newRotation := rotations[newIndex]

	// Apply with wlr-randr (force absolute orientation)
	// This makes wlr-randr and config file stay in sync
	rotateCmd := exec.CommandContext(ctx, "wlr-randr", "--output", outputName, "--transform", newRotation)
	err = rotateCmd.Run()
	if err != nil {
		c.logger.Error("Failed to rotate screen", zap.Error(err))
		return nil, fmt.Errorf("failed to rotate screen: %s", err)
	}

	// Write rotation value to file
	if err := os.WriteFile(configPath, []byte(newRotation), 0644); err != nil {
		c.logger.Warn("Failed to save screen orientation", zap.Error(err))
	}

	c.logger.Info("Screen rotated and saved",
		zap.String("output", outputName),
		zap.String("rotation", newRotation))

	c.screenInitialized = false

	orientationReplyMsg := "landscape"
	switch newRotation {
	case "90":
		orientationReplyMsg = "portraitReverse"
	case "180":
		orientationReplyMsg = "landscapeReverse"
	case "270":
		orientationReplyMsg = "portrait"
	}
	return map[string]string{"orientation": orientationReplyMsg}, nil
}

func (c *CommandHandler) handleKeyboardEvent(ctx context.Context, args []byte) (interface{}, error) {
	var cmdArgs struct {
		Code int `json:"code"`
	}

	err := json.Unmarshal(args, &cmdArgs)
	if err != nil {
		return nil, fmt.Errorf("invalid arguments: %s", err)
	}

	keyName := ""
	if cmdArgs.Code >= 32 && cmdArgs.Code <= 126 {
		keyName = string(rune(cmdArgs.Code))
	} else {
		keyName = c.mapToYdoKey(cmdArgs.Code)
	}

	c.logger.Info("Keyboard event", zap.Int("code", cmdArgs.Code), zap.String("key", keyName))

	// Prepare CDP command to dispatch a key event
	keyEventParams := map[string]interface{}{
		"type":                  "keyDown",
		"windowsVirtualKeyCode": cmdArgs.Code,
		"key":                   keyName,
		"text":                  keyName,
		"unmodifiedText":        keyName,
		"nativeVirtualKeyCode":  cmdArgs.Code,
	}

	// Send key directly via CDP
	_, err = c.cdp.SendCDPRequest("Input.dispatchKeyEvent", keyEventParams)
	if err != nil {
		c.logger.Error("Failed to send key via CDP", zap.Error(err))
		return nil, fmt.Errorf("failed to send keyboard event: %s", err)
	}

	// For keys that need keyUp events as well (like letters)
	if cmdArgs.Code >= 32 {
		keyEventParams["type"] = "keyUp"
		_, err := c.cdp.SendCDPRequest("Input.dispatchKeyEvent", keyEventParams)
		if err != nil {
			c.logger.Error("Failed to send keyUp via CDP", zap.Error(err))
		}
	}

	return CmdOK, nil
}

func (c *CommandHandler) initializeScreenDimensions(ctx context.Context) error {
	if c.screenInitialized {
		return nil
	}

	// Get screen dimensions using CDP's Runtime.evaluate
	evalParams := map[string]interface{}{
		"expression":    "({width: window.innerWidth, height: window.innerHeight})",
		"returnByValue": true,
	}

	result, err := c.cdp.SendCDPRequest("Runtime.evaluate", evalParams)
	if err != nil {
		c.logger.Error("Failed to get screen dimensions", zap.Error(err))
		// Use default values
		c.screenWidth = 1920
		c.screenHeight = 1080
	} else if result != nil {
		if dimensions, ok := result.(map[string]interface{}); ok {
			if width, ok := dimensions["width"].(float64); ok {
				c.screenWidth = width
			} else {
				c.screenWidth = 1920
			}
			if height, ok := dimensions["height"].(float64); ok {
				c.screenHeight = height
			} else {
				c.screenHeight = 1080
			}
		}
	}

	// Initialize cursor at the center of the screen
	c.cursorPositionX = c.screenWidth / 2
	c.cursorPositionY = c.screenHeight / 2
	c.screenInitialized = true
	c.movingScaleFactor = c.screenWidth / 1920

	c.logger.Info("Screen dimensions initialized",
		zap.Float64("width", c.screenWidth),
		zap.Float64("height", c.screenHeight),
		zap.Float64("cursorX", c.cursorPositionX),
		zap.Float64("cursorY", c.cursorPositionY))

	return nil
}

func (c *CommandHandler) handleMouseMoveEvent(ctx context.Context, args []byte) (interface{}, error) {
	// Initialize screen dimensions if not done already
	if err := c.initializeScreenDimensions(ctx); err != nil {
		return nil, err
	}

	// Parse cursor offsets
	var cursorArgs struct {
		MessageID     string `json:"messageID"`
		CursorOffsets []struct {
			DX float64 `json:"dx"`
			DY float64 `json:"dy"`
		} `json:"cursorOffsets"`
	}

	err := json.Unmarshal(args, &cursorArgs)
	if err != nil {
		return nil, fmt.Errorf("invalid arguments: %s", err)
	}

	// Convert relative positions to absolute positions
	absolutePositions := make([]map[string]float64, 0, len(cursorArgs.CursorOffsets))

	for i, offset := range cursorArgs.CursorOffsets {
		// Calculate the magnitude of this offset
		magnitude := math.Sqrt(offset.DX*offset.DX + offset.DY*offset.DY)

		var clampedDX, clampedDY float64

		// Only clamp obvious outliers (very large jumps)
		if magnitude > 150 {
			// This is likely a catch-up jump, clamp aggressively
			maxOffset := 25.0
			clampedDX = math.Max(-maxOffset, math.Min(maxOffset, offset.DX))
			clampedDY = math.Max(-maxOffset, math.Min(maxOffset, offset.DY))

			c.logger.Debug("Clamping outlier offset",
				zap.Int("index", i),
				zap.Float64("magnitude", magnitude),
				zap.Float64("originalDX", offset.DX),
				zap.Float64("originalDY", offset.DY),
				zap.Float64("clampedDX", clampedDX),
				zap.Float64("clampedDY", clampedDY))
		} else {
			// Normal movement, use original values
			clampedDX = offset.DX
			clampedDY = offset.DY
		}

		// Update cursor position with the offset
		c.cursorPositionX += (clampedDX * c.movingScaleFactor)
		c.cursorPositionY += (clampedDY * c.movingScaleFactor)

		// Ensure position stays within screen bounds
		c.cursorPositionX = math.Max(0, math.Min(c.cursorPositionX, c.screenWidth))
		c.cursorPositionY = math.Max(0, math.Min(c.cursorPositionY, c.screenHeight))

		// Add to absolute positions array
		absolutePositions = append(absolutePositions, map[string]float64{
			"x": c.cursorPositionX,
			"y": c.cursorPositionY,
		})
	}

	// Skip if there are no positions
	if len(absolutePositions) == 0 {
		return CmdOK, nil
	}

	// 1. Pass the entire array of absolute positions to JavaScript via CDP
	positionsJSON, err := json.Marshal(map[string]interface{}{
		"messageID": cursorArgs.MessageID,
		"message": map[string]interface{}{
			"command": "cursorUpdate",
			"request": map[string]interface{}{
				"positions": absolutePositions,
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal positions: %s", err)
	}

	// Call JavaScript function to process all positions
	_, err = c.cdp.SendCDPRequest(CDP_METHOD_EVALUATE, map[string]interface{}{
		"expression": fmt.Sprintf("window.handleCDPRequest(%s)", string(positionsJSON)),
	})
	if err != nil {
		c.logger.Error("Failed to execute JavaScript cursor positions", zap.Error(err))
		return nil, fmt.Errorf("failed to process cursor positions: %s", err)
	}

	// 2. Send the final mouse event to actually move the cursor
	if len(absolutePositions) > 0 {
		// Get the last position for the final mouseMoved event
		moveParams := map[string]interface{}{
			"type":       "mouseMoved",
			"x":          c.cursorPositionX,
			"y":          c.cursorPositionY,
			"button":     "none",
			"buttons":    0,
			"clickCount": 0,
		}

		_, err = c.cdp.SendCDPRequest("Input.dispatchMouseEvent", moveParams)
		if err != nil {
			c.logger.Error("Failed to move mouse via CDP", zap.Error(err))
			return nil, fmt.Errorf("failed to move mouse: %s", err)
		}

		c.logger.Info("Mouse moved to final position",
			zap.Float64("x", c.cursorPositionX),
			zap.Float64("y", c.cursorPositionY))
	}

	return CmdOK, nil
}

func (c *CommandHandler) handleMouseTapEvent(ctx context.Context, args []byte) (interface{}, error) {
	// Initialize screen dimensions if not done already
	if err := c.initializeScreenDimensions(ctx); err != nil {
		return nil, err
	}

	c.logger.Info("Mouse tap event at current position",
		zap.Float64("x", c.cursorPositionX),
		zap.Float64("y", c.cursorPositionY))

	// 1. Press mouse button at current position
	downParams := map[string]interface{}{
		"type":       "mousePressed",
		"x":          c.cursorPositionX,
		"y":          c.cursorPositionY,
		"button":     "left",
		"buttons":    1,
		"clickCount": 1,
	}

	_, err := c.cdp.SendCDPRequest("Input.dispatchMouseEvent", downParams)
	if err != nil {
		c.logger.Error("Failed to press mouse button via CDP", zap.Error(err))
		return nil, fmt.Errorf("failed to press mouse button: %s", err)
	}

	// 2. Release mouse button
	upParams := map[string]interface{}{
		"type":       "mouseReleased",
		"x":          c.cursorPositionX,
		"y":          c.cursorPositionY,
		"button":     "left",
		"buttons":    0,
		"clickCount": 1,
	}

	_, err = c.cdp.SendCDPRequest("Input.dispatchMouseEvent", upParams)
	if err != nil {
		c.logger.Error("Failed to release mouse button via CDP", zap.Error(err))
		return nil, fmt.Errorf("failed to release mouse button: %s", err)
	}

	return CmdOK, nil
}

func (c *CommandHandler) mapToYdoKey(keyCode int) string {
	switch keyCode {
	case 32:
		return "space"
	case 9:
		return "tab"
	case 13:
		return "return"
	case 27:
		return "escape"
	case 8:
		return "backspace"
	case 37:
		return "left"
	case 38:
		return "up"
	case 39:
		return "right"
	case 40:
		return "down"
	default:
		c.logger.Warn("Unhandled key code", zap.Int("code", keyCode))
		return ""
	}
}

func (c *CommandHandler) shutdown(ctx context.Context) (interface{}, error) {
	c.logger.Info("Executing shutdown command")

	cmd := exec.CommandContext(ctx, "sudo", "shutdown", "-h", "now")

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to execute shutdown command: %s", err)
	}

	return CmdOK, nil
}
