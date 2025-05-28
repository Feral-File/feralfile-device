package main

import (
	"context"
	"fmt"
	"reflect"

	"github.com/feral-file/godbus"
	"go.uber.org/zap"
)

// NotificationType represents the type of notification
type NotificationType string

const (
	NOTIFICATION_TYPE_SYSTEM_METRICS NotificationType = "system_metrics"
	NOTIFICATION_TYPE_PLAYER_STATUS  NotificationType = "player_status"
)

// notificationPersistConfig maps notification types to their persist record counts
var notificationPersistConfig = map[NotificationType]int{
	NOTIFICATION_TYPE_SYSTEM_METRICS: 10,
	NOTIFICATION_TYPE_PLAYER_STATUS:  1,
}

type Mediator struct {
	relayer    *RelayerClient
	dbus       *godbus.DBusClient
	cdp        *CDPClient
	cmd        *CommandHandler
	logger     *zap.Logger
	playerComm *PlayerComm
}

func NewMediator(
	relayer *RelayerClient,
	dbus *godbus.DBusClient,
	cdp *CDPClient,
	cmd *CommandHandler,
	logger *zap.Logger) *Mediator {
	return &Mediator{
		relayer: relayer,
		dbus:    dbus,
		cdp:     cdp,
		cmd:     cmd,
		logger:  logger,
	}
}

func (m *Mediator) Start() {
	m.dbus.OnBusSignal(m.handleDBusSignal)
	m.relayer.OnRelayerMessage(m.handleRelayerMessage)
}

func (m *Mediator) Stop() {
	m.relayer.RemoveRelayerMessage(m.handleRelayerMessage)
	m.dbus.RemoveBusSignal(m.handleDBusSignal)
}

func (m *Mediator) handleDBusSignal(
	ctx context.Context,
	payload godbus.DBusPayload) ([]interface{}, error) {
	if payload.Member.IsACK() {
		return nil, nil
	}

	switch payload.Member {
	case DBUS_SYS_MONITORD_EVENT_SYSMETRICS:
		if len(payload.Body) != 1 {
			m.logger.Error("Invalid number of arguments", zap.Int("expected", 1), zap.Int("actual", len(payload.Body)))
			return nil, fmt.Errorf("invalid number of arguments")
		}

		body, ok := payload.Body[0].([]byte)
		if !ok {
			m.logger.Error("Invalid body type", zap.String("expected", "[]byte"), zap.String("actual", reflect.TypeOf(payload.Body[0]).String()))
			return nil, fmt.Errorf("invalid body type")
		}

		m.logger.Debug("Received sysmetrics", zap.String("metrics", string(body)))
		m.cmd.saveLastSysMetrics(body)

		// Send system metrics notification
		m.relayer.sendNotification(ctx, NOTIFICATION_TYPE_SYSTEM_METRICS, body)

	case DBUS_SYS_MONITORD_EVENT_CONNECTIVITY_CHANGE:
		if len(payload.Body) != 1 {
			m.logger.Error("Invalid number of arguments", zap.Int("expected", 1), zap.Int("actual", len(payload.Body)))
			return nil, fmt.Errorf("invalid number of arguments")
		}

		connected, ok := payload.Body[0].(bool)
		if !ok {
			m.logger.Error("Invalid body type", zap.String("expected", "bool"), zap.String("actual", reflect.TypeOf(payload.Body[0]).String()))
			return nil, fmt.Errorf("invalid body type")
		}

		// Send the connectivity change to web app
		_, err := m.cdp.SendCDPRequest(
			CDP_METHOD_EVALUATE,
			map[string]interface{}{
				"expression": fmt.Sprintf("window.handleConnectivityChange(%t)", connected),
			})
		if err != nil {
			m.logger.Error("Failed to send CDP request", zap.Error(err))
		}

		// Reconnect the relayer if it's not already connected
		if connected && !m.relayer.IsConnected() {
			err := m.relayer.RetryableConnect(ctx)
			if err != nil {
				m.logger.Error("Failed to reconnect to relayer", zap.Error(err))
				panic(err)
			}
		}

	default:
		m.logger.Warn("Unknown signal", zap.String("member", payload.Member.String()))
	}

	return nil, nil
}

func (m *Mediator) handleRelayerMessage(ctx context.Context, payload RelayerPayload) error {
	switch payload.MessageID {
	case RELAYER_MESSAGE_ID_SYSTEM:
		topicID := payload.Message.TopicID
		if topicID == nil {
			m.logger.Error("Payload doesn't contain topicID", zap.Any("payload", payload))
			return fmt.Errorf("payload doesn't contain topicID")
		}

		// Save state
		state := GetState()
		state.Relayer.TopicID = *topicID
		err := state.Save()
		if err != nil {
			m.logger.Error("Failed to persist state", zap.Error(err))
			return err
		}
	default:
		cmd := payload.Message.Command
		if cmd == nil {
			m.logger.Warn("Received relayer message with no command", zap.Any("payload", payload))
			return nil
		}

		if cmd.CDPCmd() {
			p, err := payload.JSON()
			if err != nil {
				m.logger.Error("Failed to marshal payload", zap.Error(err))
				return err
			}

			result, err := m.cdp.SendCDPRequest(CDP_METHOD_EVALUATE, map[string]interface{}{
				"expression": fmt.Sprintf("window.handleCDPRequest(%s)", string(p)),
			})
			if err != nil {
				m.logger.Error("Failed to send CDP request", zap.Error(err))
				return err
			}

			m.playerComm.ForceRefresh()

			return m.relayer.Send(ctx, result)
		} else {
			result, err := m.cmd.Execute(ctx,
				Command{
					Command:   *cmd,
					Arguments: payload.Message.Args,
				})
			if err != nil {
				m.logger.Error("Failed to execute command", zap.Error(err))
				return err
			}

			return m.relayer.Send(ctx,
				map[string]interface{}{
					"type":      "RPC",
					"messageID": payload.MessageID,
					"message":   result,
				})
		}
	}

	return nil
}

// SetPlayerComm sets the PlayerComm reference after initialization
func (m *Mediator) SetPlayerComm(playerComm *PlayerComm) {
	m.playerComm = playerComm
}
