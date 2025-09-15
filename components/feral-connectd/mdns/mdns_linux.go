//go:build linux

package mdns

import (
	"context"
	"net"

	"github.com/vishvananda/netlink"
	"go.uber.org/zap"
)

type mdnsLinux struct {
	mdns

	linkChan chan (netlink.LinkUpdate)
}

func NewLinux(ctx context.Context, logger *zap.Logger) MDNS {
	return &mdnsLinux{
		mdns:     new(ctx, logger),
		linkChan: make(chan netlink.LinkUpdate),
	}
}

func (m *mdnsLinux) Start() error {
	err := m.mdns.Start()
	if err != nil {
		return err
	}

	err = netlink.LinkSubscribe(m.linkChan, m.done)
	if err != nil {
		return err
	}

	go m.background()

	return nil
}

func (m *mdnsLinux) background() {
	for {
		select {
		case <-m.done:
			return
		case <-m.ctx.Done():
			return
		case linkUpdate := <-m.linkChan:
			m.logger.Info("Link update", zap.Any("linkUpdate", linkUpdate))
			if linkUpdate.Flags&uint32(net.FlagUp) == 0 || linkUpdate.Flags&uint32(net.FlagLoopback) != 0 {
				continue // ignore down/lo
			}

			// Re-register all services
			m.reregister()
		}
	}
}
