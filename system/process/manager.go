package process

import (
	"context"
	"time"

	api "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
	"go.uber.org/zap"
)

// Manager orchestrates process launches by selecting hosts and delegating to them.
type Manager struct {
	node   relay.Node
	logger *zap.Logger
}

// NewManager creates a new process manager.
func NewManager(node relay.Node, logger *zap.Logger) *Manager {
	return &Manager{
		node:   node,
		logger: logger,
	}
}

// Start launches a process on the specified host.
func (m *Manager) Start(ctx context.Context, start *api.Start) (relay.PID, error) {
	// Look up host
	relayHost, exists := m.node.GetHost(start.HostID)
	if !exists {
		m.logger.Warn("host not found",
			zap.String("host_id", start.HostID),
			zap.String("source", start.Source.String()))
		return relay.PID{}, api.NewHostNotFoundError(start.HostID)
	}

	// Cast to process.Host
	host, ok := relayHost.(api.Host)
	if !ok {
		return relay.PID{}, api.NewInvalidHostError(start.HostID)
	}

	m.logger.Debug("starting process",
		zap.String("host", start.HostID),
		zap.String("source", start.Source.String()),
		zap.Int("context_pairs", len(start.Context)))

	// Delegate to host - it assigns PID internally
	return host.Run(ctx, start)
}

// Cancel sends a cancellation event to the process.
func (m *Manager) Cancel(_ context.Context, from, pid relay.PID, deadline time.Time) error {
	relayHost, exists := m.node.GetHost(pid.Host)
	if !exists {
		return api.NewHostNotFoundError(pid.Host)
	}

	if err := relayHost.Send(topology.Cancel(from, pid, deadline)); err != nil {
		m.logger.Error("failed to send cancel",
			zap.String("pid", pid.String()),
			zap.Error(err))
		return err
	}

	return nil
}

// Terminate forcefully stops a running process.
func (m *Manager) Terminate(ctx context.Context, pid relay.PID) error {
	relayHost, exists := m.node.GetHost(pid.Host)
	if !exists {
		return api.NewHostNotFoundError(pid.Host)
	}

	host, ok := relayHost.(api.Host)
	if !ok {
		return api.NewInvalidHostError(pid.Host)
	}

	return host.Terminate(ctx, pid)
}
