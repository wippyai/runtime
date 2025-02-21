package process

import (
	"context"
	"fmt"
	api "github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/topology"
	"github.com/ponyruntime/pony/internal/uniqid"
	"go.uber.org/zap"
	"time"
)

// Manager orchestrates process launches by selecting hosts, instantiating prototypes,
// and injecting topology callbacks via context.
type Manager struct {
	hosts      *HostRegistry
	prototypes *PrototypeRegistry
	topology   *Topology
	nodeID     pubsub.NodeID
	logger     *zap.Logger
	generator  *uniqid.Generator
}

// NewProcessManager creates a new Manager.
func NewProcessManager(
	hosts *HostRegistry,
	prototypes *PrototypeRegistry,
	lifecycle *Topology,
	nodeID pubsub.NodeID,
	logger *zap.Logger,
) *Manager {
	return &Manager{
		hosts:      hosts,
		prototypes: prototypes,
		topology:   lifecycle,
		nodeID:     nodeID,
		logger:     logger,
		generator:  uniqid.NewGenerator(),
	}
}

func (m *Manager) Topology() api.Topology {
	return m.topology
}

// preparePID creates and validates a pid for the process
func (m *Manager) preparePID(ps *api.StartProcess, managed bool) (pubsub.PID, error) {
	pid := pubsub.PID{
		Host:   ps.HostID,
		ID:     ps.ID,
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
func (m *Manager) launchOnHost(ctx context.Context, host api.Host, pid pubsub.PID, ps *api.StartProcess) (pubsub.PID, error) {
	m.logger.Debug("launching process",
		zap.String("host", ps.HostID),
		zap.String("pid", pid.String()),
	)

	switch h := host.(type) {
	case api.Managed:
		proc, err := m.prototypes.Create(ps.ID)
		if err != nil {
			return pubsub.PID{}, fmt.Errorf("failed to init launch: %w", err)
		}

		newPid, err := h.Launch(ctx, &api.LaunchProcess{PID: pid, Process: proc, Input: ps.Payloads})
		if err != nil {
			return pubsub.PID{}, fmt.Errorf("failed to launch process on managed host: %w", err)
		}
		return newPid, nil

	case api.Delegated:
		newPid, err := h.Launch(ctx, pid, ps.Payloads)
		if err != nil {
			return pubsub.PID{}, fmt.Errorf("failed to launch process on delegated host: %w", err)
		}
		return newPid, nil

	default:
		return pubsub.PID{}, fmt.Errorf("invalid host type: %T", host)
	}
}

// Start launches a process with basic topology management
func (m *Manager) Start(ctx context.Context, ps *api.StartProcess) (pubsub.PID, error) {
	host, exists := m.hosts.GetHost(ps.HostID)
	if !exists {
		return pubsub.PID{}, fmt.Errorf("host not found: `%s`", ps.HostID)
	}

	_, managed := host.(api.Managed)
	pid, err := m.preparePID(ps, managed)
	if err != nil {
		return pubsub.PID{}, err
	}

	return m.launchOnHost(ctx, host, pid, ps)
}

// StartMonitored launches a process with monitoring from another process
func (m *Manager) StartMonitored(ctx context.Context, from pubsub.PID, ps *api.StartProcess) (pubsub.PID, error) {
	host, exists := m.hosts.GetHost(ps.HostID)
	if !exists {
		return pubsub.PID{}, fmt.Errorf("host not found: `%s`", ps.HostID)
	}

	_, managed := host.(api.Managed)
	pid, err := m.preparePID(ps, managed)
	if err != nil {
		return pubsub.PID{}, err
	}

	if err = m.topology.monitor.Register(pid); err != nil {
		return pubsub.PID{}, fmt.Errorf("failed to register process: %w", err)
	}

	// Set up monitoring before launch
	if err = m.topology.monitor.Wait(from, pid); err != nil {
		return pubsub.PID{}, fmt.Errorf("failed to monitor process: %w", err)
	}

	return m.launchOnHost(ctx, host, pid, ps)
}

// Cancel sends a cancellation event to the process and its monitors
func (m *Manager) Cancel(ctx context.Context, from, pid pubsub.PID, deadline time.Time) error {
	if err := m.topology.upstream.Send(ctx, topology.Cancel(from, pid, deadline)); err != nil {
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
