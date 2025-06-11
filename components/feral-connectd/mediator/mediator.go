package mediator

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/Feral-File/feralfile-device/components/feral-connectd/cdp"
	"github.com/Feral-File/feralfile-device/components/feral-connectd/command"
	"github.com/Feral-File/feralfile-device/components/feral-connectd/dbus"
	"github.com/Feral-File/feralfile-device/components/feral-connectd/relayer"
	"github.com/Feral-File/feralfile-device/components/feral-connectd/state"
	"github.com/Feral-File/feralfile-device/components/feral-connectd/status"
	"github.com/feral-file/godbus"
	"go.uber.org/zap"
)

//go:generate mockgen -source=mediator.go -destination=../mocks/mock_mediator.go -package=mocks -mock_names=Interface=MockMediator

type Interface interface {
	Start()
	Stop()
	SetStatusPoller(statusPoller status.PollerInterface)
}

type Mediator struct {
	relayer      relayer.ClientInterface
	dbus         dbus.ClientInterface
	cdp          cdp.ClientInterface
	cmd          command.HandlerInterface
	statusPoller status.PollerInterface
	logger       *zap.Logger
}

func New(
	relayer relayer.ClientInterface,
	dbus dbus.ClientInterface,
	cdp cdp.ClientInterface,
	cmd command.HandlerInterface,
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

	m.logger.Info("handle received DBus signal", zap.String("name", payload.Name()), zap.String("path", payload.Path.String()))

	switch payload.Member {
	case dbus.MONITORD_EVENT_SYSMETRICS:
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
		m.cmd.SaveLastSysMetrics(body)

	case dbus.MONITORD_EVENT_CONNECTIVITY_CHANGE:
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
		_, err := m.cdp.Send(
			cdp.METHOD_EVALUATE,
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
			}
		}

	default:
		m.logger.Warn("Unknown signal", zap.String("member", payload.Member.String()))
	}

	return nil, nil
}

func (m *Mediator) handleRelayerMessage(ctx context.Context, payload relayer.Payload) error {
	m.logger.Info("handle received relayer message", zap.Any("payload", payload))

	switch payload.MessageID {
	case relayer.MESSAGE_ID_SYSTEM:
		topicID := payload.Message.TopicID
		if topicID == nil {
			m.logger.Error("Payload doesn't contain topicID", zap.Any("payload", payload))
			return fmt.Errorf("payload doesn't contain topicID")
		}

		// Save state
		s := state.GetState()
		s.Relayer.TopicID = *topicID
		err := s.Save()
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

		if cmd.ConnectdCmd() {
			result, err := m.cmd.Execute(ctx,
				command.Command{
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

		} else {
			p, err := payload.JSON()
			if err != nil {
				m.logger.Error("Failed to marshal payload", zap.Error(err))
				return err
			}

			result, err := m.cdp.Send(cdp.METHOD_EVALUATE, map[string]interface{}{
				"expression": fmt.Sprintf("window.handleCDPRequest(%s)", string(p)),
			})
			if err != nil {
				m.logger.Error("Failed to send CDP request", zap.Error(err))
				return err
			}
			time.Sleep(500 * time.Millisecond)

			m.statusPoller.ForceRefresh()

			return m.relayer.Send(ctx, result)
		}
	}

	return nil
}

// SetStatusPoller sets the StatusPoller reference after initialization
func (m *Mediator) SetStatusPoller(statusPoller status.PollerInterface) {
	m.statusPoller = statusPoller
}
