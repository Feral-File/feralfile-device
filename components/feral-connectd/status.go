package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/zap"
)

const (
	STATUS_POLL_INTERVAL = 10 * time.Second
)

// StatusPoller handles periodic polling of both player status via CDP and device status
type StatusPoller struct {
	cdp         *CDPClient
	relayer     *RelayerClient
	logger      *zap.Logger
	stopChan    chan struct{}
	refreshChan chan struct{}
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

func (s *StatusPoller) Start(ctx context.Context) {
	s.logger.Info("Starting status polling (player and device)")

	ticker := time.NewTicker(STATUS_POLL_INTERVAL)
	defer ticker.Stop()

	// Poll immediately on start
	s.pollPlayerStatus(ctx)
	s.pollDeviceStatus(ctx)

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("Status polling stopped due to context cancellation")
			return
		case <-s.stopChan:
			s.logger.Info("Status polling stopped")
			return
		case <-ticker.C:
			s.pollPlayerStatus(ctx)
			s.pollDeviceStatus(ctx)
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
	err = s.relayer.sendNotification(ctx, NOTIFICATION_TYPE_PLAYER_STATUS, result)
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
