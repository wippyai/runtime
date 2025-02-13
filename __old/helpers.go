package __old

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/runtime"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/api/service/shell"
)

func (m *RuntimeManager) unpackFunction(data payload.Payload) (*api.FunctionConfig, error) {
	cfg := new(api.FunctionConfig)

	if err := m.dtt.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal function config: %w", err)
	}

	if validator, ok := interface{}(cfg).(interface{ Validate() error }); ok {
		if err := validator.Validate(); err != nil {
			return nil, fmt.Errorf("invalid function configuration: %w", err)
		}
	}

	return cfg, nil
}

func (m *RuntimeManager) unpackLibrary(data payload.Payload) (*api.LibraryConfig, error) {
	cfg := new(api.LibraryConfig)

	if err := m.dtt.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal library config: %w", err)
	}

	if validator, ok := interface{}(cfg).(interface{ Validate() error }); ok {
		if err := validator.Validate(); err != nil {
			return nil, fmt.Errorf("invalid library configuration: %w", err)
		}
	}

	return cfg, nil
}

func (m *RuntimeManager) unpackWorkflow(data payload.Payload) (*api.WorkflowConfig, error) {
	cfg := new(api.WorkflowConfig)

	if err := m.dtt.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal workflow config: %w", err)
	}

	if validator, ok := interface{}(cfg).(interface{ Validate() error }); ok {
		if err := validator.Validate(); err != nil {
			return nil, fmt.Errorf("invalid workflow configuration: %w", err)
		}
	}

	return cfg, nil
}

func (m *RuntimeManager) unpackTerminal(data payload.Payload) (*api.TerminalConfig, error) {
	cfg := new(api.TerminalConfig)

	if err := m.dtt.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal terminal config: %w", err)
	}

	if validator, ok := interface{}(cfg).(interface{ Validate() error }); ok {
		if err := validator.Validate(); err != nil {
			return nil, fmt.Errorf("invalid terminal configuration: %w", err)
		}
	}

	return cfg, nil
}

// Helper methods for event handling
func (m *RuntimeManager) registerHandler(ctx context.Context, id registry.Name) {
	m.bus.Send(ctx, events.Event{
		System: runtime.FunctionSystem,
		Kind:   runtime.RegisterFunctionHandler,
		Path:   events.Path(id), // todo: ns?
		Data:   m.Execute,
		// todo: use itnernal map to set
	})
}

func (m *RuntimeManager) unregisterHandler(ctx context.Context, id registry.Name) { //nolint:unused
	m.bus.Send(ctx, events.Event{
		System: runtime.FunctionSystem,
		Kind:   runtime.DeleteFunctionHandler,
		Path:   events.Path(id),
	})
}

func (m *RuntimeManager) registerTerminal(ctx context.Context, id registry.Name, app shell.Terminal) {
	m.bus.Send(ctx, events.Event{
		System: shell.System,
		Kind:   shell.RegisterShell,
		Path:   events.Path(id),
		Data:   app,
	})
}

func (m *RuntimeManager) unregisterTerminal(ctx context.Context, id registry.Name) { //nolint:unused
	m.bus.Send(ctx, events.Event{
		System: shell.System,
		Kind:   shell.DeleteShell,
		Path:   events.Path(id),
		Data:   id,
	})
}

func (m *RuntimeManager) registerWorkflow(ctx context.Context, id registry.Name, runner func() any) {
	m.bus.Send(ctx, events.Event{
		System: runtime.ProcessSystem,
		Kind:   runtime.RegisterSpawnCommand,
		Path:   events.Path(id),
		Data: runtime.RegisterSpawn{
			ID:    id,
			Spawn: runner,
		},
	})
}

func (m *RuntimeManager) unregisterWorkflow(ctx context.Context, id registry.Name) {
	m.bus.Send(ctx, events.Event{
		System: runtime.ProcessSystem,
		Kind:   runtime.DeleteSpawnCommand,
		Path:   events.Path(id),
		Data:   runtime.DeleteSpawn{Target: id},
	})
}
