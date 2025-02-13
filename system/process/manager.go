package process

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/payload"
	api "github.com/ponyruntime/pony/api/process"
	"go.uber.org/zap"
)

// ProcessManager provides a unified interface for process creation and management
type ProcessManager struct {
	hosts      *HostRegistry
	prototypes *PrototypeRegistry
	logger     *zap.Logger
}

// NewProcessManager creates a new process manager instance
func NewProcessManager(hosts *HostRegistry, prototypes *PrototypeRegistry, logger *zap.Logger) *ProcessManager {
	return &ProcessManager{
		hosts:      hosts,
		prototypes: prototypes,
		logger:     logger,
	}
}

// Launch creates and starts a new process instance on the specified host
func (m *ProcessManager) Launch(ctx context.Context, p api.LaunchProcess) (api.PID, error) {
	// Get the host
	host, exists := m.hosts.GetHost(p.HostID)
	if !exists {
		return api.PID{}, fmt.Errorf("host not found: %s", p.HostID)
	}

	pid := api.PID{
		Host: p.HostID,
		ID:   p.ID,
		Name: p.Name,
	}

	// Handle different host types
	switch h := host.(type) {
	case api.Managed:
		// For managed hosts, we need to create the process first
		process, err := m.prototypes.Create(p.ID)
		if err != nil {
			return api.PID{}, fmt.Errorf("failed to create process: %w", err)
		}

		// Launch on managed host with process instance
		newPid, err := h.Launch(ctx, pid, p.Task, process)
		if err != nil {
			return api.PID{}, fmt.Errorf("failed to launch process on managed host: %w", err)
		}
		return newPid, nil

	case api.Delegated:
		// For delegated hosts, just pass the task
		newPid, err := h.Launch(ctx, pid, p.Task)
		if err != nil {
			return api.PID{}, fmt.Errorf("failed to launch process on delegated host: %w", err)
		}
		return newPid, nil

	default:
		return api.PID{}, fmt.Errorf("invalid host type: %T", host)
	}
}

// Send delivers a message to a specific process
func (m *ProcessManager) Send(ctx context.Context, pid api.PID, msg payload.Payloads) error {
	host, exists := m.hosts.GetHost(pid.Host)
	if !exists {
		return fmt.Errorf("host not found: %s", pid.Host)
	}

	return host.Send(ctx, pid, msg)
}

// Terminate stops a specific process
func (m *ProcessManager) Terminate(ctx context.Context, pid api.PID) error {
	host, exists := m.hosts.GetHost(pid.Host)
	if !exists {
		return fmt.Errorf("host not found: %s", pid.Host)
	}

	return host.Terminate(ctx, pid)
}
