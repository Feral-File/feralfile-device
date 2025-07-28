package mdns

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/grandcat/zeroconf"
	"go.uber.org/zap"
)

type MDNS interface {
	Start() error
	Stop() error
	Register(entry *MDNSEntry) error
	Unregister(id string) error
}

type mdns struct {
	mu sync.Mutex

	ctx        context.Context
	srvs       map[string]*zeroconf.Server
	srvEntries map[string]*MDNSEntry

	logger  *zap.Logger
	started bool
	done    chan struct{}
}

type MDNSEntry struct {
	ID       string
	Instance string
	Service  string
	Domain   string
	Port     int
	Text     []string
	Ifaces   []net.Interface
}

func new(ctx context.Context, logger *zap.Logger) mdns {
	return mdns{
		ctx:        ctx,
		logger:     logger,
		srvs:       make(map[string]*zeroconf.Server),
		srvEntries: make(map[string]*MDNSEntry),
		done:       make(chan struct{}),
	}
}

func (m *mdns) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return fmt.Errorf("MDNS already started")
	}

	return nil
}

func (m *mdns) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return fmt.Errorf("MDNS not started")
	}

	select {
	case <-m.done:
	default:
		close(m.done)
	}

	// Shutdown all registered servers
	for _, srv := range m.srvs {
		srv.Shutdown()
	}

	// Reset the state
	m.srvs = make(map[string]*zeroconf.Server)
	m.srvEntries = make(map[string]*MDNSEntry)
	m.started = false

	m.logger.Info("Stopped MDNS servers")

	return nil
}

// reregister re-registers all services
func (m *mdns) reregister() {
	m.logger.Info("Reregistering MDNS servers")

	m.mu.Lock()
	defer m.mu.Unlock()

	for id, srv := range m.srvs {
		// Shutdown the server
		srv.Shutdown()

		// Get the entry
		entry := m.srvEntries[id]

		// Register the service again
		srv, err := zeroconf.Register(
			entry.Instance,
			entry.Service,
			entry.Domain,
			entry.Port,
			entry.Text,
			entry.Ifaces,
		)
		if err != nil {
			m.logger.Error("Failed to register service", zap.Error(err))
			continue
		}

		m.srvs[id] = srv
	}

}

// Register a service with the given instance, service, domain, port, text, and interfaces
func (m *mdns) Register(entry *MDNSEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.srvs[entry.ID]; ok {
		return fmt.Errorf("entry already registered")
	}

	srv, err := zeroconf.Register(
		entry.Instance,
		entry.Service,
		entry.Domain,
		entry.Port,
		entry.Text,
		entry.Ifaces,
	)
	if err != nil {
		return err
	}

	m.srvs[entry.ID] = srv
	m.srvEntries[entry.ID] = entry

	return nil
}

// Unregister a service with the given instance
func (m *mdns) Unregister(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	srv, ok := m.srvs[id]
	if !ok {
		return fmt.Errorf("entry not registered")
	}

	srv.Shutdown()
	delete(m.srvs, id)
	delete(m.srvEntries, id)

	return nil
}
