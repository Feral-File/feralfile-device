package main

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	STATUS_POLL_INTERVAL = 5 * time.Second
)

// StatusPoller handles periodic polling of both player status via CDP and device status
type StatusPoller struct {
	sync.RWMutex
	cdp         *CDPClient
	relayer     *RelayerClient
	logger      *zap.Logger
	stopChan    chan struct{}
	refreshChan chan struct{}

	// Store last system metrics
	lastSysMetrics []byte

	// Store last status hashes for each notification type to avoid duplicate notifications
	lastStatusHashes map[NotificationType]string
}

func NewStatusPoller(cdp *CDPClient, relayer *RelayerClient, logger *zap.Logger) *StatusPoller {
	return &StatusPoller{
		cdp:              cdp,
		relayer:          relayer,
		logger:           logger,
		stopChan:         make(chan struct{}),
		refreshChan:      make(chan struct{}, 10), // Buffered channel to prevent blocking
		lastStatusHashes: make(map[NotificationType]string),
	}
}

// SaveLastSysMetrics stores the latest system metrics
func (s *StatusPoller) SaveLastSysMetrics(metrics []byte) {
	s.Lock()
	defer s.Unlock()
	s.lastSysMetrics = metrics
}

// GetLastSysMetrics returns the last stored system metrics
func (s *StatusPoller) GetLastSysMetrics() (map[string]interface{}, error) {
	s.RLock()
	defer s.RUnlock()

	var sysMetrics map[string]interface{}
	if s.lastSysMetrics != nil {
		err := json.Unmarshal(s.lastSysMetrics, &sysMetrics)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal last sys metrics: %s", err)
		}
	}

	return sysMetrics, nil
}

// computeStatusHash computes a fast MD5 hash of the status data for comparison
func (s *StatusPoller) computeStatusHash(data interface{}) (string, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return "", err
	}

	hash := md5.Sum(jsonData)
	return fmt.Sprintf("%x", hash), nil
}

// shouldSendNotification checks if the status has changed since last notification
// Returns true if status changed or if this is the first time checking this status type
func (s *StatusPoller) shouldSendNotification(notificationType NotificationType, data interface{}) bool {
	if data == nil {
		return false
	}

	currentHash, err := s.computeStatusHash(data)
	if err != nil {
		// If we can't compute hash, send the notification anyway
		s.logger.Warn("Failed to compute status hash, sending notification anyway",
			zap.String("type", string(notificationType)),
			zap.Error(err))
		return true
	}

	s.RLock()
	lastHash, exists := s.lastStatusHashes[notificationType]
	s.RUnlock()

	if !exists || lastHash != currentHash {
		// Only acquire write lock when we need to update
		s.Lock()
		s.lastStatusHashes[notificationType] = currentHash
		s.Unlock()
		return true
	}

	return false
}

func (s *StatusPoller) Start(ctx context.Context) {
	s.logger.Info("Starting status polling (player and device)")

	// Ticker for player and device status (every 10 seconds)
	statusTicker := time.NewTicker(STATUS_POLL_INTERVAL)
	defer statusTicker.Stop()

	// Poll immediately on start
	s.pollPlayerStatus(ctx)
	s.pollDeviceStatus(ctx)
	s.pollSysMetrics(ctx)

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("Status polling stopped due to context cancellation")
			return
		case <-s.stopChan:
			s.logger.Info("Status polling stopped")
			return
		case <-statusTicker.C:
			s.pollPlayerStatus(ctx)
			s.pollDeviceStatus(ctx)
			s.pollSysMetrics(ctx)
		case <-s.refreshChan:
			s.logger.Debug("Force refreshing status due to CDP command")
			s.pollPlayerStatus(ctx)
			s.pollDeviceStatus(ctx)
		}
	}
}

func (s *StatusPoller) Stop() {
	s.logger.Info("Stopping status polling")
	close(s.stopChan)
}

// ForceRefresh triggers an immediate status poll
func (s *StatusPoller) ForceRefresh() {
	select {
	case s.refreshChan <- struct{}{}:
		// Successfully queued refresh
	default:
		// Channel is full, skip this refresh request
		s.logger.Debug("Refresh channel full, skipping force refresh")
	}
}

func (s *StatusPoller) pollPlayerStatus(ctx context.Context) {
	// Check if relayer is connected before polling
	if !s.relayer.IsConnected() {
		s.logger.Debug("Relayer not connected, skipping player status poll")
		return
	}

	s.logger.Debug("Polling player status from Chromium")

	// Create the payload in the same format as mediator
	payload := map[string]interface{}{
		"messageID": "",
		"message": map[string]interface{}{
			"command": "checkStatus",
			"request": map[string]interface{}{},
		},
	}

	// Marshal the payload to JSON string
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		s.logger.Error("Failed to marshal checkStatus payload", zap.Error(err))
		return
	}

	// Send CDP request using the same format as mediator
	result, err := s.cdp.SendCDPRequest(CDP_METHOD_EVALUATE, map[string]interface{}{
		"expression": fmt.Sprintf("window.handleCDPRequest(%s)", string(payloadBytes)),
	})
	if err != nil {
		s.logger.Error("Failed to get player status from CDP", zap.Error(err))
		return
	}

	// Send the status as a notification
	resultMap, ok := result.(map[string]interface{})
	if !ok {
		s.logger.Error("Failed to convert result to map", zap.Any("result", result))
		return
	}
	message, ok := resultMap["message"]
	if !ok {
		s.logger.Error("Result map does not contain message key", zap.Any("result", result))
		return
	}

	// Check if we should send this notification
	if !s.shouldSendNotification(NOTIFICATION_TYPE_PLAYER_STATUS, message) {
		s.logger.Debug("Player status unchanged, skipping notification")
		return
	}

	err = s.relayer.sendNotification(ctx, NOTIFICATION_TYPE_PLAYER_STATUS, message)
	if err != nil {
		s.logger.Error("Failed to send player status notification", zap.Error(err))
	}
}

func (s *StatusPoller) pollDeviceStatus(ctx context.Context) {
	// Check if relayer is connected before polling
	if !s.relayer.IsConnected() {
		s.logger.Debug("Relayer not connected, skipping device status poll")
		return
	}

	s.logger.Debug("Polling device status")

	// Get device status using the shared function
	deviceStatus, err := GetDeviceStatus(ctx)
	if err != nil {
		s.logger.Error("Failed to get device status", zap.Error(err))
		return
	}

	// Check if we should send this notification
	if !s.shouldSendNotification(NOTIFICATION_TYPE_DEVICE_STATUS, deviceStatus) {
		s.logger.Debug("Device status unchanged, skipping notification")
		return
	}

	// Send the device status as a notification
	err = s.relayer.sendNotification(ctx, NOTIFICATION_TYPE_DEVICE_STATUS, deviceStatus)
	if err != nil {
		s.logger.Error("Failed to send device status notification", zap.Error(err))
	}
}

func (s *StatusPoller) pollSysMetrics(ctx context.Context) {
	// Check if relayer is connected before polling
	if !s.relayer.IsConnected() {
		s.logger.Debug("Relayer not connected, skipping sys metrics poll")
		return
	}

	s.logger.Debug("Polling sys metrics")

	// Get sys metrics from our stored data
	sysMetrics, err := s.GetLastSysMetrics()
	if err != nil {
		s.logger.Error("Failed to get sys metrics", zap.Error(err))
		return
	}

	// Check if we should send this notification (now includes sys metrics filtering)
	if !s.shouldSendNotification(NOTIFICATION_TYPE_SYSTEM_METRICS, sysMetrics) {
		s.logger.Debug("System metrics unchanged, skipping notification")
		return
	}

	err = s.relayer.sendNotification(ctx, NOTIFICATION_TYPE_SYSTEM_METRICS, sysMetrics)
	if err != nil {
		s.logger.Error("Failed to send sys metrics notification", zap.Error(err))
	}
}
