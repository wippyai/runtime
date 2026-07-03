// SPDX-License-Identifier: MPL-2.0

package env

import (
	"context"

	"github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	envsvc "github.com/wippyai/runtime/api/service/env"
	entryutil "github.com/wippyai/runtime/internal/entry"
	sysenv "github.com/wippyai/runtime/system/env"
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
	if log == nil {
		log = zap.NewNop()
	}
	return &VariableManager{
		log: log,
		dtt: dtt,
		bus: bus,
	}
}

func (m *VariableManager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != envsvc.Variable {
		return sysenv.NewUnsupportedKindError(entry.Kind)
	}

	variable, err := entryutil.DecodeEntryConfig[env.Variable](ctx, m.dtt, entry)
	if err != nil {
		return sysenv.NewDecodeVariableError(err)
	}

	// Register directly in the central registry for synchronous access.
	if reg := env.GetRegistry(ctx); reg != nil {
		if err := reg.RegisterVariable(*variable); err != nil {
			return err
		}
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
	if entry.Kind != envsvc.Variable {
		return sysenv.NewUnsupportedKindError(entry.Kind)
	}

	variable, err := entryutil.DecodeEntryConfig[env.Variable](ctx, m.dtt, entry)
	if err != nil {
		return sysenv.NewDecodeVariableError(err)
	}

	// Apply directly in the central registry for synchronous access.
	if reg := env.GetRegistry(ctx); reg != nil {
		if err := reg.RegisterVariable(*variable); err != nil {
			return err
		}
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
	if entry.Kind != envsvc.Variable {
		return sysenv.NewUnsupportedKindError(entry.Kind)
	}

	// Unregister directly from the central registry for synchronous access.
	if reg := env.GetRegistry(ctx); reg != nil {
		reg.UnregisterVariable(entry.ID)
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
