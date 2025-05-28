package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/zap"
)

const (
	PLAYER_STATUS_POLL_INTERVAL = 5 * time.Second
)

type PlayerComm struct {
	cdp         *CDPClient
	mediator    *Mediator
	logger      *zap.Logger
	stopChan    chan struct{}
	refreshChan chan struct{}
}

func NewPlayerComm(cdp *CDPClient, mediator *Mediator, logger *zap.Logger) *PlayerComm {
	return &PlayerComm{
		cdp:         cdp,
		mediator:    mediator,
		logger:      logger,
		stopChan:    make(chan struct{}),
		refreshChan: make(chan struct{}, 10), // Buffered channel to prevent blocking
	}
}

func (p *PlayerComm) Start(ctx context.Context) {
	p.logger.Info("Starting player status polling")

	ticker := time.NewTicker(PLAYER_STATUS_POLL_INTERVAL)
	defer ticker.Stop()

	// Poll immediately on start
	p.pollPlayerStatus(ctx)

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("Player status polling stopped due to context cancellation")
			return
		case <-p.stopChan:
			p.logger.Info("Player status polling stopped")
			return
		case <-ticker.C:
			p.pollPlayerStatus(ctx)
		case <-p.refreshChan:
			p.logger.Debug("Force refreshing player status due to CDP command")
			p.pollPlayerStatus(ctx)
		}
	}
}

func (p *PlayerComm) Stop() {
	p.logger.Info("Stopping player status polling")
	close(p.stopChan)
}

// ForceRefresh triggers an immediate status poll
func (p *PlayerComm) ForceRefresh() {
	select {
	case p.refreshChan <- struct{}{}:
		// Successfully queued refresh
	default:
		// Channel is full, skip this refresh request
		p.logger.Debug("Refresh channel full, skipping force refresh")
	}
}

func (p *PlayerComm) pollPlayerStatus(ctx context.Context) {
	p.logger.Debug("Polling player status from Chromium")

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
		p.logger.Error("Failed to marshal checkStatus payload", zap.Error(err))
		return
	}

	// Send CDP request using the same format as mediator
	result, err := p.cdp.SendCDPRequest(CDP_METHOD_EVALUATE, map[string]interface{}{
		"expression": fmt.Sprintf("window.handleCDPRequest(%s)", string(payloadBytes)),
	})
	if err != nil {
		p.logger.Error("Failed to get player status from CDP", zap.Error(err))
		return
	}

	// Send the status as a notification
	err = p.mediator.sendNotification(ctx, NOTIFICATION_TYPE_PLAYER_STATUS, result)
	if err != nil {
		p.logger.Error("Failed to send player status notification", zap.Error(err))
	}
}
