package process

import (
	"context"
	"fmt"
	api "github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/runtime"
	"go.uber.org/zap"
)

type Manager struct {
	hosts      *HostRegistry
	prototypes *PrototypeRegistry
	logger     *zap.Logger
}

func NewProcessManager(hosts *HostRegistry, prototypes *PrototypeRegistry, logger *zap.Logger) *Manager {
	return &Manager{
		hosts:      hosts,
		prototypes: prototypes,
		logger:     logger,
	}
}

func (m *Manager) Start(ctx context.Context, pl api.Start) (api.PID, error) {
	host, exists := m.hosts.GetHost(pl.HostID)
	if !exists {
		return api.PID{}, fmt.Errorf("host not found: `%s`", pl.HostID)
	}

	pid := api.PID{
		Host: pl.HostID,
		ID:   pl.ID,
		Name: pl.Name,
	}

	switch h := host.(type) {
	case api.Managed:
		launch, err := m.initLaunch(pid, pl)
		if err != nil {
			return api.PID{}, fmt.Errorf("failed to init launch: %w", err)
		}

		newPid, err := h.Launch(ctx, launch)
		if err != nil {
			return api.PID{}, fmt.Errorf("failed to launch process on managed host: %w", err)
		}
		return newPid, nil

	case api.Delegated:
		newPid, err := h.Launch(ctx, pid, pl.Payloads)
		if err != nil {
			return api.PID{}, fmt.Errorf("failed to launch process on delegated host: %w", err)
		}
		return newPid, nil

	default:
		return api.PID{}, fmt.Errorf("invalid host type: %T", host)
	}
}

func (m *Manager) initLaunch(pid api.PID, pl api.Start) (api.Launch, error) {
	process, err := m.prototypes.Create(pl.ID)
	if err != nil {
		return api.Launch{}, err
	}

	return api.Launch{
		PID:     pid,
		Process: process,
		Input:   pl.Payloads,
		OnComplete: []api.OnComplete{
			func(pid api.PID, result *runtime.Result) {
				if result.Error != nil {
					m.logger.Error("process failed",
						zap.String("id", pl.ID.String()),
						zap.Error(result.Error))
				} else {
					m.logger.Info("process completed",
						zap.String("id", pl.ID.String()))
				}
				if result.Payload != nil {
					m.logger.Info("process result",
						zap.String("id", pl.ID.String()),
						zap.Any("payload", result.Payload))
				}
			},
		},
	}, nil
}

func (m *Manager) Send(ctx context.Context, pid api.PID, msg *api.Message) error {
	host, exists := m.hosts.GetHost(pid.Host)
	if !exists {
		return fmt.Errorf("host not found: %s", pid.Host)
	}

	return host.Send(ctx, pid, msg)
}

func (m *Manager) Terminate(ctx context.Context, pid api.PID) error {
	host, exists := m.hosts.GetHost(pid.Host)
	if !exists {
		return fmt.Errorf("host not found: %s", pid.Host)
	}

	return host.Terminate(ctx, pid)
}
