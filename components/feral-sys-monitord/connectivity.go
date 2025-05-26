package main

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

const (
	// Ping interval in seconds
	PING_INTERVAL = 30 * time.Second

	// Connection timeout
	BACKGROUND_PING_TIMEOUT = 5 * time.Second
	RPC_PING_TIMEOUT        = 2 * time.Second
)

var PING_TARGET_ADDRESS = []string{
	"1.1.1.1:443", // Cloudflare
	"8.8.8.8:443", // Google
}

type ConnectivityHandler func(ctx context.Context, connected bool)

type Connectivity struct {
	sync.Mutex

	ctx           context.Context
	logger        *zap.Logger
	handlers      []ConnectivityHandler
	done          chan struct{}
	lastConnected bool
}

func NewConnectivity(ctx context.Context, logger *zap.Logger) *Connectivity {
	return &Connectivity{
		ctx:           ctx,
		logger:        logger,
		handlers:      []ConnectivityHandler{},
		done:          make(chan struct{}),
		lastConnected: false,
	}
}

func (c *Connectivity) GetLastConnected() bool {
	c.Lock()
	defer c.Unlock()
	return c.lastConnected
}

func (c *Connectivity) Start() {
	c.logger.Info("Starting Connectivity Watcher")
	c.background()
}

func (c *Connectivity) Stop() {
	select {
	case <-c.done:
		c.logger.Info("Connectivity Watcher already stopped")
	default:
		close(c.done)
	}
	c.logger.Info("Connectivity Watcher stopped")
}

func (c *Connectivity) OnConnectivityChange(handler ConnectivityHandler) {
	c.Lock()
	defer c.Unlock()
	c.handlers = append(c.handlers, handler)
}

func (c *Connectivity) RemoveConnectivityChange(h ConnectivityHandler) {
	c.Lock()
	defer c.Unlock()

	for i, handler := range c.handlers {
		if fmt.Sprintf("%p", handler) == fmt.Sprintf("%p", h) {
			c.handlers = append(c.handlers[:i], c.handlers[i+1:]...)
			break
		}
	}
}

// notifyHandlers notifies all registered handlers about connectivity status
func (c *Connectivity) notifyHandlers(ctx context.Context, connected bool) {
	c.Lock()
	handlers := make([]ConnectivityHandler, len(c.handlers))
	copy(handlers, c.handlers)
	c.Unlock()

	for _, handler := range handlers {
		go func(h ConnectivityHandler) {
			select {
			case <-ctx.Done():
				return
			case <-c.done:
				return
			default:
				h(ctx, connected)
			}
		}(handler)
	}
}

func (c *Connectivity) background() {
	go func() {
		c.logger.Info("Connectivity background goroutine started")

		// Check initial connectivity
		connected, err := c.CheckConnectivity(BACKGROUND_PING_TIMEOUT)
		if err != nil {
			c.logger.Warn("Connectivity check failed", zap.Error(err))
		}
		c.notifyHandlers(c.ctx, connected)

		ticker := time.NewTicker(PING_INTERVAL)
		defer ticker.Stop()

		for {
			select {
			case <-c.ctx.Done():
				c.logger.Info("Connectivity background goroutine stopped")
				return
			case <-c.done:
				c.logger.Info("Connectivity Watcher stopped")
				return
			case <-ticker.C:
				c.logger.Info("Checking connectivity")
				connected, err := c.CheckConnectivity(BACKGROUND_PING_TIMEOUT)
				if err != nil {
					c.logger.Warn("Connectivity check failed", zap.Error(err))
					continue
				}
				c.Lock()
				if connected != c.lastConnected {
					c.notifyHandlers(c.ctx, connected)
					c.lastConnected = connected
				}
				c.Unlock()
				c.logger.Info("Connectivity check result", zap.Bool("connected", connected))
			}
		}
	}()
}

// CheckConnectivity attempts to connect to the PING_TARGET address to check connectivity
func (c *Connectivity) CheckConnectivity(timeout time.Duration) (bool, error) {
	ctx, cancel := context.WithTimeout(c.ctx, timeout+1*time.Second)
	defer cancel()

	eg, egCtx := errgroup.WithContext(ctx)
	resultChan := make(chan bool, len(PING_TARGET_ADDRESS))
	defer close(resultChan)

	for _, target := range PING_TARGET_ADDRESS {
		target := target
		eg.Go(func() error {
			before := time.Now()
			dialer := net.Dialer{Timeout: timeout}
			conn, err := dialer.DialContext(egCtx, "tcp", target)
			after := time.Now()
			c.logger.Debug("Connectivity check result", zap.String("target", target), zap.Duration("duration", after.Sub(before)), zap.Error(err))
			if conn != nil {
				conn.Close()
			}

			select {
			case resultChan <- err == nil:
			case <-egCtx.Done():
				return nil
			case <-c.done:
				return nil
			case <-c.ctx.Done():
				return nil
			}

			return err
		})
	}

	err := eg.Wait()
	if err != nil {
		return false, err
	}

	connected := false
	for i := 0; i < len(PING_TARGET_ADDRESS); i++ {
		result := <-resultChan
		if result {
			connected = true
			break
		}
	}

	return connected, nil
}
