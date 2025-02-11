package function

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/api/registry"
	api "github.com/ponyruntime/pony/api/runtime/lua"
	lua "github.com/ponyruntime/pony/runtime/lua/code"
	"github.com/ponyruntime/pony/runtime/lua/factory"
	"go.uber.org/zap"
)

type Manager struct {
	log  *zap.Logger
	code *lua.Manager
}

func NewManager(log *zap.Logger, code *lua.Manager) *Manager {
	return &Manager{log: log, code: code}
}

func NewFunctionManager(log *zap.Logger, code *lua.Manager) *factory.Handler {
	return factory.NewHandler(api.KindFunction, NewManager(log, code))
}

func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindFunction {
		return fmt.Errorf("invalid entry kind %s, expected %s", entry.Kind, api.KindFunction)
	}

	cfg, err := factory.UnpackConfig[api.FunctionConfig](ctx, entry)
	if err != nil {
		return fmt.Errorf("failed to unpack function config: %w", err)
	}

	node := lua.Node{
		ID:     entry.ID,
		Kind:   api.KindFunction,
		Source: cfg.Source,
		Method: cfg.Method,
	}

	imports := factory.BuildImports(append(cfg.Modules, cfg.Libraries...), cfg.ImportAliases)

	if err := m.code.AddNode(ctx, node, imports); err != nil {
		return fmt.Errorf("failed to add function node: %w", err)
	}

	// todo: compile and register? or defer?

	return nil
}

func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindFunction {
		return fmt.Errorf("invalid entry kind %s, expected %s", entry.Kind, api.KindFunction)
	}

	cfg, err := factory.UnpackConfig[api.FunctionConfig](ctx, entry)
	if err != nil {
		return fmt.Errorf("failed to unpack function config: %w", err)
	}

	node := lua.Node{
		ID:     entry.ID,
		Kind:   api.KindFunction,
		Source: cfg.Source,
		Method: cfg.Method,
	}

	imports := factory.BuildImports(append(cfg.Modules, cfg.Libraries...), cfg.ImportAliases)

	if err := m.code.UpdateNode(ctx, node, imports); err != nil {
		return fmt.Errorf("failed to update function node: %w", err)
	}

	return nil
}

func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	if entry.Kind != api.KindFunction {
		return fmt.Errorf("invalid entry kind %s, expected %s", entry.Kind, api.KindFunction)
	}

	if err := m.code.DeleteNode(ctx, entry.ID); err != nil {
		return fmt.Errorf("failed to delete function node: %w", err)
	}

	return nil
}

func (m *Manager) Invalidate(ids []registry.ID) {
	for _, id := range ids {
		m.log.Debug("invalidating function", zap.String("id", id.String()))
		// todo: reset values in cache
	}
}
