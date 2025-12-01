package process2

import (
	"context"
	"fmt"
	"time"

	api "github.com/wippyai/runtime/api/process2"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
	"go.uber.org/zap"
)

// Manager orchestrates process launches by selecting hosts and delegating to them.
type Manager struct {
	node   relay.Node
	logger *zap.Logger

	mutators []api.StartMutator
}

// NewManager creates a new process manager.
func NewManager(node relay.Node, logger *zap.Logger) *Manager {
	return &Manager{
		node:     node,
		logger:   logger,
		mutators: make([]api.StartMutator, 0),
	}
}

// RegisterMutator adds a StartMutator to be executed during Start().
func (m *Manager) RegisterMutator(mutator api.StartMutator) {
	m.mutators = append(m.mutators, mutator)
}

// Start launches a process on the specified host.
func (m *Manager) Start(ctx context.Context, start *api.Start) (relay.PID, error) {
	// Run mutators to modify start request
	var err error
	for _, mutator := range m.mutators {
		ctx, err = mutator(ctx, start)
		if err != nil {
			return relay.PID{}, fmt.Errorf("mutator failed: %w", err)
		}
	}

	// Look up host
	relayHost, exists := m.node.GetHost(start.HostID)
	if !exists {
		m.logger.Warn("host not found",
			zap.String("host_id", start.HostID),
			zap.String("source", start.Source.String()))
		return relay.PID{}, fmt.Errorf("host not found: %s", start.HostID)
	}

	// Cast to process2.Host
	host, ok := relayHost.(api.Host)
	if !ok {
		return relay.PID{}, fmt.Errorf("host %s does not implement process2.Host", start.HostID)
	}

	m.logger.Debug("starting process",
		zap.String("host", start.HostID),
		zap.String("source", start.Source.String()))

	// Delegate to host - it assigns PID internally
	return host.Run(ctx, start)
}

// Cancel sends a cancellation event to the process.
func (m *Manager) Cancel(_ context.Context, from, pid relay.PID, deadline time.Time) error {
	relayHost, exists := m.node.GetHost(pid.Host)
	if !exists {
		return fmt.Errorf("host not found: %s", pid.Host)
	}

	if err := relayHost.Send(topology.Cancel(from, pid, deadline)); err != nil {
		m.logger.Error("failed to send cancel",
			zap.String("pid", pid.String()),
			zap.Error(err))
	}

	return nil
}

// Terminate forcefully stops a running process.
func (m *Manager) Terminate(ctx context.Context, pid relay.PID) error {
	relayHost, exists := m.node.GetHost(pid.Host)
	if !exists {
		return fmt.Errorf("host not found: %s", pid.Host)
	}

	host, ok := relayHost.(api.Host)
	if !ok {
		return fmt.Errorf("host %s does not implement process2.Host", pid.Host)
	}

	return host.Terminate(ctx, pid)
}
