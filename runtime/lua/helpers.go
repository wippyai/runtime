package lua

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/executor"
	"github.com/ponyruntime/pony/api/process"

	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	"github.com/ponyruntime/pony/api/service/terminal"
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
func (m *RuntimeManager) registerHandler(ctx context.Context, id registry.ID) {
	m.bus.Send(ctx, events.Event{
		System: executor.System,
		Kind:   executor.RegisterHandlerEvent,
		Path:   events.Path(id),
		Data:   executor.RegisterHandler{Target: id, Handler: m.Execute},
	})
}

func (m *RuntimeManager) unregisterHandler(ctx context.Context, id registry.ID) { //nolint:unused
	m.bus.Send(ctx, events.Event{
		System: executor.System,
		Kind:   executor.DeleteHandlerEvent,
		Path:   events.Path(id),
		Data:   executor.DeleteHandler{Target: id},
	})
}

func (m *RuntimeManager) registerTerminal(ctx context.Context, id registry.ID, app terminal.Terminal) {
	m.bus.Send(ctx, events.Event{
		System: terminal.System,
		Kind:   terminal.RegisterTerminalEvent,
		Path:   events.Path(id),
		Data:   app,
	})
}

func (m *RuntimeManager) unregisterTerminal(ctx context.Context, id registry.ID) { //nolint:unused
	m.bus.Send(ctx, events.Event{
		System: terminal.System,
		Kind:   terminal.DeleteTerminalEvent,
		Path:   events.Path(id),
		Data:   id,
	})
}

func (m *RuntimeManager) registerWorkflow(ctx context.Context, id registry.ID, runner func() any) {
	m.bus.Send(ctx, events.Event{
		System: process.WorkflowSystem,
		Kind:   process.RegisterWorkflowEvent,
		Path:   events.Path(id),
		Data: process.RegisterWorkflow{
			Target:  id,
			Handler: runner,
		},
	})
}

func (m *RuntimeManager) unregisterWorkflow(ctx context.Context, id registry.ID) {
	m.bus.Send(ctx, events.Event{
		System: process.WorkflowSystem,
		Kind:   process.DeleteWorkflowEvent,
		Path:   events.Path(id),
		Data:   process.DeleteWorkflow{Target: id},
	})
}
