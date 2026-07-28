// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
	apierror "github.com/wippyai/runtime/api/error"
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
	if start == nil {
		return pid.PID{}, apierror.New(api.InvalidState, "start command is required").WithRetryable(apierror.False)
	}
	if m.node == nil {
		return pid.PID{}, apierror.New(api.InvalidState, "process node is required").WithRetryable(apierror.False)
	}

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

	// Apply the registered frame-context resolvers (e.g. the network overlay)
	// generically; the manager stays agnostic of any specific subsystem.
	var err error
	if start.Context, err = ctxapi.FrameResolversFrom(ctx).Resolve(ctx, start.Options, start.Context); err != nil {
		return pid.PID{}, err
	}

	m.logger.Debug("starting process",
		zap.String("host", start.HostID),
		zap.String("source", start.Source.String()),
		zap.Int("context_pairs", len(start.Context)))

	// Delegate to host - it assigns PID internally
	procPID, err := host.Run(ctx, start)
	if err != nil {
		return procPID, err
	}

	if start.Options != nil && m.node != nil {
		parent, ok := start.Options.Get(api.ProcessParentKey)
		if ok {
			if parentPID, ok := parent.(pid.PID); ok && parentPID.UniqID != "" {
				if procPID.Node != "" && procPID.Node != m.node.ID() {
					topo := topology.GetTopology(ctx)
					if topo == nil {
						return procPID, NewTopologyNotAvailableError()
					}

					monitored := false
					if start.Options.GetBool(api.ProcessMonitorKey, false) {
						if err := topo.Monitor(parentPID, procPID); err != nil {
							_ = host.Terminate(context.WithoutCancel(ctx), procPID)
							return procPID, err
						}
						monitored = true
					}

					if start.Options.GetBool(api.ProcessLinkKey, false) {
						if err := topo.Link(parentPID, procPID); err != nil {
							if monitored {
								_ = topo.Demonitor(parentPID, procPID)
							}
							_ = host.Terminate(context.WithoutCancel(ctx), procPID)
							return procPID, err
						}
					}
				}
			}
		}
	}

	return procPID, nil
}

// Cancel sends a cancellation event to the process, carrying a reason.
func (m *Manager) Cancel(ctx context.Context, from, pidArg pid.PID, reason string) error {
	if pidArg.Node != "" && m.node != nil && pidArg.Node != m.node.ID() {
		if router := relay.GetRouter(ctx); router != nil {
			if err := router.Send(topology.CancelPackage(from, pidArg, reason)); err != nil {
				m.logger.Error("failed to send remote cancel",
					zap.String("pid", pidArg.String()),
					zap.Error(err))
				return err
			}
			return nil
		}
	}

	relayHost, exists := m.node.GetHost(pidArg.Host)
	if !exists {
		return NewHostNotFoundError(pidArg.Host)
	}

	if err := relayHost.Send(topology.CancelPackage(from, pidArg, reason)); err != nil {
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
