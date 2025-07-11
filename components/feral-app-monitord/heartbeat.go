package main

import (
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/zap"
)

// HeartbeatData represents the data part of the payload.
type HeartbeatData struct {
	MACAddress     string  `json:"mac"`
	Timestamp      int64   `json:"ts"`
	Build          string  `json:"build"`
	CPUTemperature float64 `json:"cpu_temp"`
}

// HeartbeatPayload is the structure for the final JSON object.
type HeartbeatPayload struct {
	Data      *HeartbeatData `json:"data"`
	PublicKey string         `json:"pubkey"`
	Signature string         `json:"signature"`
}

// SendHeartbeat orchestrates the process of sending a heartbeat.
func SendHeartbeat() {
	cpuTemp := GetCpuTemp()
	logger.Info("Gathered data", zap.Float64("Temp", cpuTemp))

	message := &HeartbeatData{
		MACAddress:     config.MAC,
		Timestamp:      time.Now().UnixMilli(),
		Build:          fmt.Sprintf("%s-%s", config.Branch, config.Version),
		CPUTemperature: cpuTemp,
	}
	messageJSON, err := json.Marshal(message)
	if err != nil {
		logger.Error("Failed to marshal message to JSON: %v", zap.Error(err))
		return
	}

	signatureHex, err := SignMessage(messageJSON)
	if err != nil {
		logger.Error("Failed to sign message", zap.Error(err))
		return
	}
	logger.Info("Message signed successfully.")

	finalPayload := &HeartbeatPayload{
		Data:      message,
		PublicKey: config.Pubkey,
		Signature: signatureHex,
	}
	finalPayloadJSON, err := json.Marshal(finalPayload)
	if err != nil {
		logger.Error("Failed to marshal final payload", zap.Error(err))
		return
	}

	if err := SendPayload(finalPayloadJSON); err != nil {
		logger.Error("Failed to send payload", zap.Error(err))
		return
	}

	logger.Info("Heartbeat sent successfully.")
}
