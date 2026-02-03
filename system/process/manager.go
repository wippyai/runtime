package process

import (
	"context"
	"time"

	"github.com/wippyai/runtime/api/pid"
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
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Manager{
		node:   node,
		logger: logger,
	}
}

// Start launches a process on the specified host.
func (m *Manager) Start(ctx context.Context, start *api.Start) (pid.PID, error) {
	// Look up host
	relayHost, exists := m.node.GetHost(start.HostID)
	if !exists {
		m.logger.Warn("host not found",
			zap.String("host_id", start.HostID),
			zap.String("source", start.Source.String()))
		return pid.PID{}, NewHostNotFoundError(start.HostID)
	}

	// Cast to process.Host
	host, ok := relayHost.(api.Host)
	if !ok {
		return pid.PID{}, NewInvalidHostError(start.HostID)
	}

	m.logger.Debug("starting process",
		zap.String("host", start.HostID),
		zap.String("source", start.Source.String()),
		zap.Int("context_pairs", len(start.Context)))

	// Delegate to host - it assigns PID internally
	return host.Run(ctx, start)
}

// Cancel sends a cancellation event to the process.
func (m *Manager) Cancel(_ context.Context, from, pidArg pid.PID, deadline time.Time) error {
	relayHost, exists := m.node.GetHost(pidArg.Host)
	if !exists {
		return NewHostNotFoundError(pidArg.Host)
	}

	if err := relayHost.Send(topology.CancelPackage(from, pidArg, deadline)); err != nil {
		m.logger.Error("failed to send cancel",
			zap.String("pid", pidArg.String()),
			zap.Error(err))
		return err
	}

	return nil
}

// Terminate forcefully stops a running process.
func (m *Manager) Terminate(ctx context.Context, pidArg pid.PID) error {
	relayHost, exists := m.node.GetHost(pidArg.Host)
	if !exists {
		return NewHostNotFoundError(pidArg.Host)
	}

	host, ok := relayHost.(api.Host)
	if !ok {
		return NewInvalidHostError(pidArg.Host)
	}

	return host.Terminate(ctx, pidArg)
}
