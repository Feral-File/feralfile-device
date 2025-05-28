package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	STATUS_POLL_INTERVAL      = 10 * time.Second
	SYS_METRICS_POLL_INTERVAL = 5 * time.Second
)

// StatusPoller handles periodic polling of both player status via CDP and device status
type StatusPoller struct {
	sync.Mutex
	cdp         *CDPClient
	relayer     *RelayerClient
	logger      *zap.Logger
	stopChan    chan struct{}
	refreshChan chan struct{}

	// Store last system metrics
	lastSysMetrics []byte
}

func NewStatusPoller(cdp *CDPClient, relayer *RelayerClient, logger *zap.Logger) *StatusPoller {
	return &StatusPoller{
		cdp:         cdp,
		relayer:     relayer,
		logger:      logger,
		stopChan:    make(chan struct{}),
		refreshChan: make(chan struct{}, 10), // Buffered channel to prevent blocking
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
	s.Lock()
	defer s.Unlock()

	var sysMetrics map[string]interface{}
	if s.lastSysMetrics != nil {
		err := json.Unmarshal(s.lastSysMetrics, &sysMetrics)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal last sys metrics: %s", err)
		}
	}

	return sysMetrics, nil
}

func (s *StatusPoller) Start(ctx context.Context) {
	s.logger.Info("Starting status polling (player and device)")

	// Ticker for player and device status (every 10 seconds)
	statusTicker := time.NewTicker(STATUS_POLL_INTERVAL)
	defer statusTicker.Stop()

	// Ticker for sys metrics (every 5 seconds)
	sysMetricsTicker := time.NewTicker(SYS_METRICS_POLL_INTERVAL)
	defer sysMetricsTicker.Stop()

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
		case <-sysMetricsTicker.C:
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

	// Send the sys metrics as a notification
	err = s.relayer.sendNotification(ctx, NOTIFICATION_TYPE_SYSTEM_METRICS, sysMetrics)
	if err != nil {
		s.logger.Error("Failed to send sys metrics notification", zap.Error(err))
	}
}

// GetSysMetrics is a shared function that can be used by both StatusPoller and CommandHandler
func GetSysMetrics(ctx context.Context) (map[string]interface{}, error) {
	// This function will be called by the command handler
	// We need to get the StatusPoller instance to access the metrics
	// For now, we'll return an error indicating this needs to be refactored
	return nil, fmt.Errorf("GetSysMetrics needs to be called through StatusPoller instance")
}
