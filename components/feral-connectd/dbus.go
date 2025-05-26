package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/feral-file/godbus"
	"github.com/godbus/dbus/v5"
)

const (
	DBUS_INTERFACE godbus.Interface = "com.feralfile.connectd.general"
	DBUS_PATH      godbus.Path      = "/com/feralfile/connectd"
	DBUS_NAME      string           = "com.feralfile.connectd"

	DBUS_SETUPD_EVENT_SHOW_PAIRING_QR_CODE      godbus.Member = "show_pairing_qr_code"
	DBUS_SYS_MONITORD_EVENT_SYSMETRICS          godbus.Member = "sysmetrics"
	DBUS_SYS_MONITORD_EVENT_CONNECTIVITY_CHANGE godbus.Member = "connectivity_change"

	RPC_TIMEOUT = 5 * time.Second
)

type ConnectdDBus struct {
	ctx     context.Context
	relayer *RelayerClient
}

func NewConnectdDBus(ctx context.Context, relayer *RelayerClient) *ConnectdDBus {
	return &ConnectdDBus{
		ctx:     ctx,
		relayer: relayer,
	}
}

func (c *ConnectdDBus) GetRelayerTopicID() (string, *dbus.Error) {
	topicID := GetState().Relayer.TopicID
	if topicID != "" {
		return topicID, nil
	}

	// Context for timeout
	deadlineCtx, cancel := context.WithTimeout(c.ctx, RPC_TIMEOUT)
	defer cancel()

	// Channel to signal when the topicID is received
	doneChan := make(chan struct{})
	errChan := make(chan error)

	// Temporary handler to receive the topicID
	var closeOnce sync.Once
	var handler RelayerHandler
	handler = func(ctx context.Context, payload RelayerPayload) error {
		var err error
		defer func() {
			if err != nil {
				errChan <- err
			}
		}()

		if payload.MessageID == RELAYER_MESSAGE_ID_SYSTEM {
			topicID := payload.Message.TopicID
			if topicID == nil {
				err = fmt.Errorf("payload doesn't contain topicID")
				return err
			}

			// Save state
			state := GetState()
			state.Relayer.TopicID = *topicID
			err = state.Save()
			if err != nil {
				return err
			}

			// Remove handler and close doneChan
			closeOnce.Do(func() {
				close(doneChan)
			})
			c.relayer.RemoveRelayerMessage(handler)
		}
		return nil
	}

	// Add handler to relayer
	c.relayer.OnRelayerMessage(handler)
	defer c.relayer.RemoveRelayerMessage(handler)

	// Connect to the relayer
	err := c.relayer.Connect(deadlineCtx)
	if errors.Is(err, errRelayerAlreadyConnected) {
		return GetState().Relayer.TopicID, nil
	}
	if err != nil {
		return "", dbus.NewError(err.Error(), []interface{}{})
	}

	// Wait for the topicID to be received or an error to occur
	for {
		select {
		case <-doneChan:
			return GetState().Relayer.TopicID, nil
		case err := <-errChan:
			return "", dbus.NewError(err.Error(), []interface{}{})
		case <-deadlineCtx.Done():
			return "", dbus.NewError(deadlineCtx.Err().Error(), []interface{}{})
		}
	}
}
