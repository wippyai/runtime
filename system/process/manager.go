package process

import (
	"context"
	"fmt"
	api "github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"go.uber.org/zap"
)

// Manager orchestrates process launches by selecting hosts, instantiating prototypes,
// and injecting lifecycle callbacks via context. In the future, plugins can register their
// own onStart and onComplete callbacks which are added here.
type Manager struct {
	hosts      *HostRegistry
	prototypes *PrototypeRegistry
	lifecycle  *Lifecycle
	nodeID     pubsub.NodeID
	logger     *zap.Logger
	generator  *UniqIDGenerator
}

// NewProcessManager creates a new Manager.
func NewProcessManager(
	hosts *HostRegistry,
	prototypes *PrototypeRegistry,
	lifecycle *Lifecycle,
	nodeID pubsub.NodeID,
	logger *zap.Logger,
) *Manager {
	m := &Manager{
		hosts:      hosts,
		prototypes: prototypes,
		lifecycle:  lifecycle,
		nodeID:     nodeID,
		logger:     logger,
		generator:  NewUniqIDGenerator(),
	}

	return m
}

func (m *Manager) Lifecycle() *Lifecycle {
	return m.lifecycle
}

// Start launches a process. It updates the context with the manager's plugin callbacks,
// then delegates the launch to the appropriate host.
func (m *Manager) Start(ctx context.Context, ps api.StartProcess) (pubsub.PID, error) {
	host, exists := m.hosts.GetHost(ps.HostID)
	if !exists {
		return pubsub.PID{}, fmt.Errorf("host not found: `%s`", ps.HostID)
	}

	pid := pubsub.PID{
		Host:   ps.HostID,
		ID:     ps.ID,
		UniqID: ps.UniqID,
	}

	if pid.UniqID == "" {
		pid.UniqID = m.generator.Generate()
	}

	switch h := host.(type) {
	case api.Managed:
		pid.Node = m.nodeID

		proc, err := m.prototypes.Create(ps.ID)
		if err != nil {
			return pubsub.PID{}, fmt.Errorf("failed to init launch: %w", err)
		}

		// attach process into common lifecycle for node level processes and workflows
		ctx = m.lifecycle.AttachToContext(ctx, pid)
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

// Terminate stops a running process.
func (m *Manager) Terminate(ctx context.Context, pid pubsub.PID) error {
	host, exists := m.hosts.GetHost(pid.Host)
	if !exists {
		return fmt.Errorf("host not found: %s", pid.Host)
	}
	return host.Terminate(ctx, pid)
}
