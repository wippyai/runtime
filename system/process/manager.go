package process

import (
	"context"
	"fmt"
	"time"

	"github.com/wippyai/runtime/api/pidgen"
	api "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
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
	mutators   []api.StartMutator
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
		mutators:   make([]api.StartMutator, 0),
	}
}

// RegisterMutator adds a StartMutator to be executed during Start()
func (m *Manager) RegisterMutator(mutator api.StartMutator) {
	m.mutators = append(m.mutators, mutator)
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
		zap.String("id", ps.Source.String()),
	)

	switch h := host.(type) {
	case api.Managed:
		proc, err := m.prototypes.Create(ps.Source)
		if err != nil {
			return relay.PID{}, fmt.Errorf("failed to init launch: %w", err)
		}

		newPid, err := h.Launch(ctx, &api.Launch{
			PID:        pid,
			Source:     ps.Source,
			Process:    proc,
			Input:      ps.Input,
			Context:    ps.Context,
			Options:    ps.Options,
			OnStart:    ps.OnStart,
			OnComplete: ps.OnComplete,
		})
		if err != nil {
			return relay.PID{}, fmt.Errorf("failed to launch process on managed host: %w", err)
		}
		return newPid, nil

	case api.Delegated:
		// Construct Lifecycle from Options for Delegated hosts
		var lifecycle api.Lifecycle
		if parent, ok := ps.Options.Get(api.LifecycleParentKey); ok {
			if pid, ok := parent.(relay.PID); ok {
				lifecycle.Parent = pid
			}
		}
		lifecycle.Monitor = ps.Options.GetBool(api.LifecycleMonitorKey, false)
		lifecycle.Link = ps.Options.GetBool(api.LifecycleLinkKey, false)

		newPid, err := h.Dispatch(ctx, lifecycle, &api.Dispatch{
			PID:     pid,
			Source:  ps.Source,
			Input:   ps.Input,
			Context: ps.Context,
			Options: ps.Options,
		})
		if err != nil {
			return relay.PID{}, fmt.Errorf("failed to dispatch process to delegated host: %w", err)
		}
		return newPid, nil

	default:
		return relay.PID{}, fmt.Errorf("invalid host type: %T", host)
	}
}

// Start launches a process, passing the lifecycle information to the host
func (m *Manager) Start(ctx context.Context, start *api.Start) (relay.PID, error) {
	// Run mutators to modify start request and thread context
	var err error
	for _, mutator := range m.mutators {
		ctx, err = mutator(ctx, start)
		if err != nil {
			return relay.PID{}, fmt.Errorf("mutator failed: %w", err)
		}
	}

	host, exists := m.hosts.GetHost(start.HostID)
	if !exists {
		return relay.PID{}, fmt.Errorf("host not found: `%s`", start.HostID)
	}

	_, managed := host.(api.Managed)
	pid := m.preparePID(ctx, start, managed)

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
