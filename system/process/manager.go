package process

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/wippyai/runtime/api/pidgen"
	api "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/api/topology"
	"go.uber.org/zap"
)

// HostLookup defines an interface for finding hosts by ID
type HostLookup interface {
	GetHost(hostID string) (api.Host, bool)
}

// Manager orchestrates process launches by selecting hosts, instantiating prototypes,
// and propagating lifecycle information to the hosts.
type Manager struct {
	hosts      HostLookup
	prototypes api.Factory
	nodeID     relay.NodeID
	logger     *zap.Logger
}

// NewProcessManager creates a new Manager.
func NewProcessManager(
	hosts HostLookup,
	prototypes api.Factory,
	nodeID relay.NodeID,
	logger *zap.Logger,
) *Manager {
	return &Manager{
		hosts:      hosts,
		prototypes: prototypes,
		nodeID:     nodeID,
		logger:     logger,
	}
}

// preparePID creates and validates a pid for the process
func (m *Manager) preparePID(ctx context.Context, ps *api.Start, managed bool) relay.PID {
	// If UniqID is already provided, construct PID directly
	if ps.UniqID != "" {
		pid := relay.PID{
			Host:   ps.HostID,
			UniqID: ps.UniqID,
		}

		if managed {
			pid.Node = m.nodeID
		}

		return pid.Precomputed()
	}

	// Use centralized PID generator
	gen := pidgen.GetGenerator(ctx)
	return gen.Generate(ps.HostID, ps.Source)
}

// launchOnHost handles the actual process launch on either managed or delegated hosts
func (m *Manager) launchOnHost(ctx context.Context, host api.Host, pid relay.PID, ps *api.Start) (relay.PID, error) {
	m.logger.Debug("launching process",
		zap.String("host", ps.HostID),
		zap.String("pid", pid.String()),
	)

	switch h := host.(type) {
	case api.Managed:
		proc, err := m.prototypes.Create(ps.Source)
		if err != nil {
			return relay.PID{}, fmt.Errorf("failed to init launch: %w", err)
		}

		// Pass the lifecycle information to the managed host
		newPid, err := h.Launch(ctx, &api.Launch{
			PID:       pid,
			Source:    ps.Source,
			Process:   proc,
			Input:     ps.Input,
			Lifecycle: ps.Lifecycle,
			Context:   ps.Context,
		})
		if err != nil {
			return relay.PID{}, fmt.Errorf("failed to launch process on managed host: %w", err)
		}
		return newPid, nil

	case api.Delegated:
		// For delegated hosts, we don't pass the Process instance
		// But we should consider adding a way to pass lifecycle info to delegated hosts as well
		newPid, err := h.Launch(ctx, pid, ps.Lifecycle, ps.Input)
		if err != nil {
			return relay.PID{}, fmt.Errorf("failed to launch process on delegated host: %w", err)
		}
		return newPid, nil

	default:
		return relay.PID{}, fmt.Errorf("invalid host type: %T", host)
	}
}

// Start launches a process, passing the lifecycle information to the host
func (m *Manager) Start(ctx context.Context, start *api.Start) (relay.PID, error) {
	host, exists := m.hosts.GetHost(start.HostID)
	if !exists {
		return relay.PID{}, fmt.Errorf("host not found: `%s`", start.HostID)
	}

	_, managed := host.(api.Managed)
	pid := m.preparePID(ctx, start, managed)

	// The topology registration and monitoring/linking will be handled by the host
	// during the actual process launch, so we don't need to do it here anymore.
	// This prevents having orphaned PIDs in the topology if the launch fails.

	return m.launchOnHost(ctx, host, pid, start)
}

// Cancel sends a cancellation event to the process and its monitors
func (m *Manager) Cancel(_ context.Context, from, pid relay.PID, deadline time.Time) error {
	host, exists := m.hosts.GetHost(pid.Host)
	if !exists {
		return fmt.Errorf("host not found: %s", pid.Host)
	}

	if err := host.Send(topology.Cancel(from, pid, deadline)); err != nil {
		m.logger.Error("failed to send cancel event",
			zap.String("pid", pid.String()),
			zap.Error(err))
	}

	return nil
}

// Terminate forcefully stops a running process
func (m *Manager) Terminate(ctx context.Context, pid relay.PID) error {
	host, exists := m.hosts.GetHost(pid.Host)
	if !exists {
		return fmt.Errorf("host not found: %s", pid.Host)
	}

	return host.Terminate(ctx, pid)
}

// AttachLifecycle returns a context with topology callbacks attached for the specified lifecycle
func (m *Manager) AttachLifecycle(ctx context.Context, lifecycle api.Lifecycle) context.Context {
	// OnStart callback adds topology integration when a process starts
	if err := api.SetOnStart(ctx, func(pid relay.PID, _ api.Process) {
		// Get topology from context
		topo := topology.GetTopology(ctx)
		if topo == nil {
			m.logger.Error("topology not found in context",
				zap.String("pid", pid.String()))
			return
		}

		m.logger.Debug("process started",
			zap.String("pid", pid.String()))

		// Register the Target with topology
		err := topo.Register(pid)

		if err != nil {
			m.logger.Warn("failed to register pid for monitoring",
				zap.String("pid", pid.String()),
				zap.Error(err))
			return
		}

		// Set up monitoring if requested and Parent Target is provided
		if lifecycle.Monitor && lifecycle.Parent.String() != "" {
			if err = topo.Wait(lifecycle.Parent, pid); err != nil {
				m.logger.Warn("failed to monitor process",
					zap.String("pid", pid.String()),
					zap.String("caller", lifecycle.Parent.String()),
					zap.Error(err))
			}
		}

		// Set up linking if requested and Parent Target is provided
		if lifecycle.Link && lifecycle.Parent.String() != "" {
			if err = topo.Link(lifecycle.Parent, pid); err != nil {
				m.logger.Warn("failed to link process",
					zap.String("pid", pid.String()),
					zap.String("caller", lifecycle.Parent.String()),
					zap.Error(err))
			}
		}
	}); err != nil {
		m.logger.Error("failed to set onStart callback", zap.Error(err))
		return ctx
	}

	// OnComplete callback handles process termination topology events
	if err := api.SetOnComplete(ctx, func(pid relay.PID, result *runtime.Result) {
		// Get topology from context
		topo := topology.GetTopology(ctx)
		if topo == nil {
			m.logger.Error("topology not found in context",
				zap.String("pid", pid.String()))
			return
		}

		// Get pid registry from context
		pidReg := topology.GetRegistry(ctx)
		if pidReg == nil {
			m.logger.Error("pid registry not found in context",
				zap.String("pid", pid.String()))
			return
		}

		if result.Error != nil {
			if errors.Is(result.Error, supervisor.ErrExit) {
				m.logger.Debug("process exited",
					zap.String("pid", pid.String()))

				result.Error = nil // normal exit
			} else {
				m.logger.Debug("process failed",
					zap.String("pid", pid.String()),
					zap.Error(result.Error))
			}
		} else {
			m.logger.Debug("process completed",
				zap.String("pid", pid.String()),
				zap.Any("result", result.Value))
		}

		topo.Notify(pid, result)
		pidReg.Remove(pid)
		topo.Remove(pid)
	}); err != nil {
		m.logger.Error("failed to set onComplete callback", zap.Error(err))
		return ctx
	}

	return ctx
}
