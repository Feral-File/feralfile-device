package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/feral-file/godbus"
	"github.com/godbus/dbus/v5"
	"go.uber.org/zap"
)

const (
	DBUS_INTERFACE godbus.Interface = "com.feralfile.connectd.general"
	DBUS_PATH      godbus.Path      = "/com/feralfile/connectd"
	DBUS_NAME      string           = "com.feralfile.connectd"

	MONITORD_DBUS_INTERFACE                      godbus.Interface = "com.feralfile.sysmonitord"
	MONITORD_DBUS_PATH                           godbus.Path      = "/com/feralfile/sysmonitord"
	MONITORD_DBUS_NAME                           string           = "com.feralfile.sysmonitord"
	MONITORD_DBUS_METHOD_GET_CONNECTIVITY_STATUS godbus.Member    = "GetConnectivityStatus"

	DBUS_SETUPD_EVENT_SHOW_PAIRING_QR_CODE      godbus.Member = "show_pairing_qr_code"
	DBUS_SYS_MONITORD_EVENT_SYSMETRICS          godbus.Member = "sysmetrics"
	DBUS_SYS_MONITORD_EVENT_CONNECTIVITY_CHANGE godbus.Member = "connectivity_change"
)

type ConnectdDBus struct {
	ctx     context.Context
	relayer *RelayerClient
	logger  *zap.Logger
}

func NewConnectdDBus(ctx context.Context, relayer *RelayerClient, logger *zap.Logger) *ConnectdDBus {
	return &ConnectdDBus{
		ctx:     ctx,
		relayer: relayer,
		logger:  logger,
	}
}

func (c *ConnectdDBus) GetRelayerTopicID() (string, *dbus.Error) {
	c.logger.Info("DBus RPC called: GetRelayerTopicID")

	topicID := GetState().Relayer.TopicID
	if topicID != "" {
		return topicID, nil
	}

	// Create a child context with deadline from the global context
	deadlineCtx, deadlineCancel := context.WithTimeout(c.ctx, 30*time.Second)
	defer deadlineCancel()

	// Create a context that will be canceled when either the deadline is reached or the global context is canceled
	retryCtx, retryCancel := context.WithCancel(c.ctx)
	_ = retryCancel // Explicitly ignore retryCancel for successful case

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

	// Connect to the relayer using the retry context
	err := c.relayer.RetryableConnect(retryCtx)
	if err != nil {
		retryCancel()
		return "", dbus.NewError(err.Error(), []interface{}{})
	}

	// Wait for the topicID to be received or an error to occur
	for {
		select {
		case <-doneChan:
			return GetState().Relayer.TopicID, nil
		case err := <-errChan:
			retryCancel()
			return "", dbus.NewError(err.Error(), []interface{}{})
		case <-deadlineCtx.Done():
			retryCancel() // Cancel the retry context when deadline is reached
			return "", dbus.NewError(deadlineCtx.Err().Error(), []interface{}{})
		}
	}
}
