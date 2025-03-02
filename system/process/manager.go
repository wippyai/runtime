package process

import (
	"context"
	"errors"
	"fmt"
	api "github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/api/topology"
	"github.com/ponyruntime/pony/internal/uniqid"
	"go.uber.org/zap"
	"time"
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
	nodeID     pubsub.NodeID
	logger     *zap.Logger
	generator  *uniqid.Generator
}

// NewProcessManager creates a new Manager.
func NewProcessManager(
	hosts HostLookup,
	prototypes api.Factory,
	nodeID pubsub.NodeID,
	logger *zap.Logger,
) *Manager {
	return &Manager{
		hosts:      hosts,
		prototypes: prototypes,
		nodeID:     nodeID,
		logger:     logger,
		generator:  uniqid.NewGenerator(),
	}
}

// preparePID creates and validates a pid for the process
func (m *Manager) preparePID(ps *api.Start, managed bool) (pubsub.PID, error) {
	pid := pubsub.PID{
		Host:   ps.HostID,
		ID:     ps.Source,
		UniqID: ps.UniqID,
	}

	if managed {
		pid.Node = m.nodeID
	}

	if pid.UniqID == "" {
		pid.UniqID = m.generator.Generate()
	}

	return pid, nil
}

// launchOnHost handles the actual process launch on either managed or delegated hosts
func (m *Manager) launchOnHost(ctx context.Context, host api.Host, pid pubsub.PID, ps *api.Start) (pubsub.PID, error) {
	m.logger.Debug("launching process",
		zap.String("host", ps.HostID),
		zap.String("pid", pid.String()),
	)

	switch h := host.(type) {
	case api.Managed:
		proc, err := m.prototypes.Create(ps.Source)
		if err != nil {
			return pubsub.PID{}, fmt.Errorf("failed to init launch: %w", err)
		}

		// Pass the lifecycle information to the managed host
		newPid, err := h.Launch(ctx, &api.Launch{
			PID:       pid,
			Process:   proc,
			Input:     ps.Input,
			Lifecycle: ps.Lifecycle,
		})
		if err != nil {
			return pubsub.PID{}, fmt.Errorf("failed to launch process on managed host: %w", err)
		}
		return newPid, nil

	case api.Delegated:
		// For delegated hosts, we don't pass the Process instance
		// But we should consider adding a way to pass lifecycle info to delegated hosts as well
		newPid, err := h.Launch(ctx, pid, ps.Input)
		if err != nil {
			return pubsub.PID{}, fmt.Errorf("failed to launch process on delegated host: %w", err)
		}
		return newPid, nil

	default:
		return pubsub.PID{}, fmt.Errorf("invalid host type: %T", host)
	}
}

// Start launches a process, passing the lifecycle information to the host
func (m *Manager) Start(ctx context.Context, ps *api.Start) (pubsub.PID, error) {
	host, exists := m.hosts.GetHost(ps.HostID)
	if !exists {
		return pubsub.PID{}, fmt.Errorf("host not found: `%s`", ps.HostID)
	}

	_, managed := host.(api.Managed)
	pid, err := m.preparePID(ps, managed)
	if err != nil {
		return pubsub.PID{}, err
	}

	// The topology registration and monitoring/linking will be handled by the host
	// during the actual process launch, so we don't need to do it here anymore.
	// This prevents having orphaned PIDs in the topology if the launch fails.

	return m.launchOnHost(ctx, host, pid, ps)
}

// Cancel sends a cancellation event to the process and its monitors
func (m *Manager) Cancel(_ context.Context, from, pid pubsub.PID, deadline time.Time) error {
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
func (m *Manager) Terminate(ctx context.Context, pid pubsub.PID) error {
	host, exists := m.hosts.GetHost(pid.Host)
	if !exists {
		return fmt.Errorf("host not found: %s", pid.Host)
	}

	return host.Terminate(ctx, pid)
}

// AttachLifecycle returns a context with topology callbacks attached for the specified lifecycle
func (m *Manager) AttachLifecycle(ctx context.Context, lifecycle api.Lifecycle) context.Context {
	// OnStart callback adds topology integration when a process starts
	ctx = api.WithAddedOnStart(ctx, func(pid pubsub.PID, proc api.Process) {
		// Get topology from context
		topo := topology.GetTopology(ctx)
		if topo == nil {
			m.logger.Error("topology not found in context",
				zap.String("pid", pid.String()))
			return
		}

		m.logger.Debug("process started",
			zap.String("pid", pid.String()))

		// Register the PID with topology
		err := topo.Register(pid)

		if err != nil {
			m.logger.Warn("failed to register pid for monitoring",
				zap.String("pid", pid.String()),
				zap.Error(err))
			return
		}

		// Set up monitoring if requested and Parent PID is provided
		if lifecycle.Monitor && lifecycle.Parent.String() != "" {
			if err = topo.Wait(lifecycle.Parent, pid); err != nil {
				m.logger.Warn("failed to monitor process",
					zap.String("pid", pid.String()),
					zap.String("caller", lifecycle.Parent.String()),
					zap.Error(err))
			}
		}

		// Set up linking if requested and Parent PID is provided
		if lifecycle.Link && lifecycle.Parent.String() != "" {
			if err = topo.Link(lifecycle.Parent, pid); err != nil {
				m.logger.Warn("failed to link process",
					zap.String("pid", pid.String()),
					zap.String("caller", lifecycle.Parent.String()),
					zap.Error(err))
			}
		}
	})

	// OnComplete callback handles process termination topology events
	ctx = api.WithAddedOnComplete(ctx, func(pid pubsub.PID, result *runtime.Result) {
		// Get topology from context
		topo := topology.GetTopology(ctx)
		if topo == nil {
			m.logger.Error("topology not found in context",
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
				zap.Any("result", result.Payload))
		}

		// Handle completion/failure notification
		topo.Notify(pid, result)
		topo.Remove(pid)
	})

	return ctx
}
