package process

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/payload"
	api "github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/runtime"
	"go.uber.org/zap"
	"log"
)

// Manager provides a unified interface for process creation and management
type Manager struct {
	hosts      *HostRegistry
	prototypes *PrototypeRegistry
	logger     *zap.Logger
}

// NewProcessManager creates a new process manager instance
func NewProcessManager(hosts *HostRegistry, prototypes *PrototypeRegistry, logger *zap.Logger) *Manager {
	return &Manager{
		hosts:      hosts,
		prototypes: prototypes,
		logger:     logger,
	}
}

// Launch creates and starts a new process instance on the specified host
func (m *Manager) Launch(ctx context.Context, pl api.Launch) (api.PID, error) {
	// Get the host
	host, exists := m.hosts.GetHost(pl.HostID)
	if !exists {
		return api.PID{}, fmt.Errorf("host not found: `%s`", pl.HostID)
	}

	pid := api.PID{
		Host: pl.HostID,
		ID:   pl.ID,
		Name: pl.Name,
	}

	// Handle different host types
	switch h := host.(type) {
	case api.Managed:
		// For managed hosts, we need to create the process first
		process, err := m.initProcess(ctx, pl)
		if err != nil {
			return api.PID{}, fmt.Errorf("failed to create process: %w", err)
		}

		// Launch on managed host with process instance
		newPid, err := h.Launch(ctx, pid, process, pl.Payloads)
		if err != nil {
			return api.PID{}, fmt.Errorf("failed to launch process on managed host: %w", err)
		}
		return newPid, nil

	case api.Delegated:
		// For delegated hosts, just pass the task
		newPid, err := h.Launch(ctx, pid, pl.Payloads)
		if err != nil {
			return api.PID{}, fmt.Errorf("failed to launch process on delegated host: %w", err)
		}
		return newPid, nil

	default:
		return api.PID{}, fmt.Errorf("invalid host type: %T", host)
	}
}

func (m *Manager) initProcess(ctx context.Context, p api.Launch) (api.Process, error) {
	prototype, err := m.prototypes.Create(p.ID)
	if err != nil {
		return nil, err
	}

	prototype.OnComplete(func(pid api.PID, result runtime.Result) {
		if result.Error != nil {
			m.logger.Error("process failed", zap.String("id", p.ID.String()), zap.Error(result.Error))
		} else {
			m.logger.Info("process completed", zap.String("id", p.ID.String()))
		}

		log.Printf("%+v", result.Payload)
	})

	// todo: perform various linkage operations

	return prototype, nil
}

// Send delivers a message to a specific process
func (m *Manager) Send(ctx context.Context, pid api.PID, msg payload.Payloads) error {
	host, exists := m.hosts.GetHost(pid.Host)
	if !exists {
		return fmt.Errorf("host not found: %s", pid.Host)
	}

	return host.Send(ctx, pid, msg)
}

// Terminate stops a specific process
func (m *Manager) Terminate(ctx context.Context, pid api.PID) error {
	host, exists := m.hosts.GetHost(pid.Host)
	if !exists {
		return fmt.Errorf("host not found: %s", pid.Host)
	}

	return host.Terminate(ctx, pid)
}
