package env

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	envsvc "github.com/wippyai/runtime/api/service/env"
	entryutil "github.com/wippyai/runtime/internal/entry"
	"go.uber.org/zap"
)

type VariableManager struct {
	log *zap.Logger
	dtt payload.Transcoder
	bus event.Bus
}

func NewVariableManager(
	bus event.Bus,
	dtt payload.Transcoder,
	log *zap.Logger,
) *VariableManager {
	return &VariableManager{
		log: log,
		dtt: dtt,
		bus: bus,
	}
}

func (m *VariableManager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != envsvc.KindVariable {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	variable, err := entryutil.DecodeEntryConfig[env.Variable](ctx, m.dtt, entry)
	if err != nil {
		return fmt.Errorf("failed to decode variable: %w", err)
	}

	m.bus.Send(ctx, event.Event{
		System: env.System,
		Kind:   env.VariableRegister,
		Path:   entry.ID.String(),
		Data:   *variable,
	})

	m.log.Debug("registered environment variable",
		zap.String("id", entry.ID.String()),
		zap.String("name", variable.Name))

	return nil
}

func (m *VariableManager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != envsvc.KindVariable {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	variable, err := entryutil.DecodeEntryConfig[env.Variable](ctx, m.dtt, entry)
	if err != nil {
		return fmt.Errorf("failed to decode variable: %w", err)
	}

	m.bus.Send(ctx, event.Event{
		System: env.System,
		Kind:   env.VariableUpdate,
		Path:   entry.ID.String(),
		Data:   *variable,
	})

	m.log.Debug("updated environment variable",
		zap.String("id", entry.ID.String()),
		zap.String("name", variable.Name))

	return nil
}

func (m *VariableManager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != envsvc.KindVariable {
		return fmt.Errorf("unsupported entry kind: %s", entry.Kind)
	}

	m.bus.Send(ctx, event.Event{
		System: env.System,
		Kind:   env.VariableDelete,
		Path:   entry.ID.String(),
	})

	m.log.Debug("deleted environment variable",
		zap.String("id", entry.ID.String()))

	return nil
}
