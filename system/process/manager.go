package process

import (
	"context"
	"fmt"
	api "github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/runtime"
	"go.uber.org/zap"
)

// Manager orchestrates process launches by selecting hosts, instantiating prototypes,
// and injecting lifecycle callbacks via context. In the future, plugins can register their
// own onStart and onComplete callbacks which are added here.
type Manager struct {
	hosts      *HostRegistry
	prototypes *PrototypeRegistry
	logger     *zap.Logger
	onStart    []api.OnStart
	onComplete []api.OnComplete
}

// NewProcessManager creates a new Manager.
func NewProcessManager(hosts *HostRegistry, prototypes *PrototypeRegistry, logger *zap.Logger) *Manager {
	m := &Manager{
		hosts:      hosts,
		prototypes: prototypes,
		logger:     logger,
	}

	m.registerLayer(
		func(pid api.PID, proc api.Process) {
			logger.Info("process started", zap.String("pid", pid.String()))
		},
		func(pid api.PID, result *runtime.Result) {
			logger.Info("process completed", zap.String("pid", pid.String()), zap.Any("result", result))
		},
	)

	return m
}

// Start launches a process. It updates the context with the manager's plugin callbacks,
// then delegates the launch to the appropriate host.
func (m *Manager) Start(ctx context.Context, ps api.StartProcess) (api.PID, error) {
	host, exists := m.hosts.GetHost(ps.HostID)
	if !exists {
		return api.PID{}, fmt.Errorf("host not found: `%s`", ps.HostID)
	}

	pid := api.PID{
		Host: ps.HostID,
		ID:   ps.ID,
		Name: ps.Name,
	}

	// Inject plugin callbacks into the context. todo: use composite one
	for _, cb := range m.onStart {
		ctx = api.WithOnStart(ctx, cb)
	}
	for _, cb := range m.onComplete {
		ctx = api.WithOnComplete(ctx, cb)
	}

	switch h := host.(type) {
	case api.Managed:
		launch, err := m.initManagedLaunch(pid, ps)
		if err != nil {
			return api.PID{}, fmt.Errorf("failed to init launch: %w", err)
		}

		newPid, err := h.Launch(ctx, launch)
		if err != nil {
			return api.PID{}, fmt.Errorf("failed to launch process on managed host: %w", err)
		}

		return newPid, nil

	case api.Delegated:
		newPid, err := h.Launch(ctx, pid, ps.Payloads)
		if err != nil {
			return api.PID{}, fmt.Errorf("failed to launch process on delegated host: %w", err)
		}
		return newPid, nil

	default:
		return api.PID{}, fmt.Errorf("invalid host type: %T", host)
	}
}

// initManagedLaunch instantiates a new process from a prototype and creates a launch configuration.
// Note: Inline callbacks have been removed in favor of using context for lifecycle events.
func (m *Manager) initManagedLaunch(pid api.PID, pl api.StartProcess) (*api.LaunchProcess, error) {
	proc, err := m.prototypes.Create(pl.ID)
	if err != nil {
		return nil, err
	}

	return &api.LaunchProcess{PID: pid, Process: proc, Input: pl.Payloads}, nil
}

// registerLayer allows plugins to register callbacks to be injected into the process context.
// Each plugin can provide an onStart and/or an onComplete callback.
func (m *Manager) registerLayer(onStart api.OnStart, onComplete api.OnComplete) {
	if onStart != nil {
		m.onStart = append(m.onStart, onStart)
	}
	if onComplete != nil {
		m.onComplete = append(m.onComplete, onComplete)
	}
}

// Send delivers a message to a running process.
func (m *Manager) Send(ctx context.Context, pid api.PID, msg *api.Message) error {
	host, exists := m.hosts.GetHost(pid.Host)
	if !exists {
		return fmt.Errorf("host not found: %s", pid.Host)
	}
	return host.Send(ctx, pid, msg)
}

// Terminate stops a running process.
func (m *Manager) Terminate(ctx context.Context, pid api.PID) error {
	host, exists := m.hosts.GetHost(pid.Host)
	if !exists {
		return fmt.Errorf("host not found: %s", pid.Host)
	}
	return host.Terminate(ctx, pid)
}
