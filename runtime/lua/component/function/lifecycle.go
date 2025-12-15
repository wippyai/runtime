package function

import (
	"context"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	api "github.com/wippyai/runtime/api/runtime/lua"
	runtimelua "github.com/wippyai/runtime/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/component"
	"go.uber.org/zap"
)

func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case api.Function:
		return m.addSource(ctx, entry)
	case api.FunctionBytecode:
		return m.addBytecode(ctx, entry)
	default:
		return api.NewInvalidEntryKindError(entry.Kind, api.Function)
	}
}

func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case api.Function:
		return m.updateSource(ctx, entry)
	case api.FunctionBytecode:
		return m.updateBytecode(ctx, entry)
	default:
		return api.NewInvalidEntryKindError(entry.Kind, api.Function)
	}
}

func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case api.Function, api.FunctionBytecode:
		if err := m.code.DeleteNode(ctx, entry.ID); err != nil {
			return runtimelua.NewDeleteNodeError("function", err)
		}
		m.removePool(entry.ID)
		m.deleteConfig(entry.ID)
		m.unregisterCaller(ctx, entry.ID)
		m.log.Debug("function deleted", zap.String("id", entry.ID.String()))
		return nil
	default:
		return api.NewInvalidEntryKindError(entry.Kind, api.Function)
	}
}

// Invalidate handles code invalidation for hot reload.
func (m *Manager) Invalidate(_ context.Context, ids []registry.ID) {
	for _, id := range ids {
		cfg := m.getConfig(id)
		if cfg == nil {
			continue
		}

		m.log.Debug("invalidating function", zap.String("id", id.String()))

		// For bytecode, verify the file is still valid before recreating
		if cfg.bytecode != nil {
			if _, err := component.LoadAndVerifyBytecode(m.fsRegistry, cfg.bytecode.FS, cfg.bytecode.Path, cfg.bytecode.Hash); err != nil {
				m.log.Error("failed to reload bytecode", zap.Error(err))
				continue
			}
		}

		if err := m.replacePool(id, cfg); err != nil {
			m.log.Error("failed to invalidate function", zap.Error(err))
		}
	}
}

// Execute runs a function with given task.
func (m *Manager) Execute(ctx context.Context, task runtime.Task) (*runtime.Result, error) {
	m.mu.RLock()
	entry, exists := m.pools[task.ID]
	m.mu.RUnlock()

	if !exists {
		return nil, api.NewPoolNotFoundError(task.ID.String())
	}

	if len(task.Context) > 0 {
		fc := ctxapi.FrameFromContext(ctx)
		if fc != nil {
			for _, pair := range task.Context {
				_ = fc.Set(pair.Key, pair.Value)
			}
		}
	}

	result, err := entry.pool.Call(ctx, entry.method, task.Payloads)
	return result, err
}

// addSource adds a source-based function.
func (m *Manager) addSource(ctx context.Context, entry registry.Entry) error {
	cfg, err := component.UnpackConfig[api.FunctionConfig](ctx, entry)
	if err != nil {
		return runtimelua.NewUnpackConfigError("function", err)
	}

	node := code.Node{
		ID:     entry.ID,
		Kind:   api.Function,
		Source: cfg.Source,
		Method: cfg.Method,
	}
	imports := component.BuildImports(cfg.Imports, cfg.Modules)
	if err := m.code.AddNode(ctx, node, imports); err != nil {
		return runtimelua.NewAddNodeError("function", err)
	}

	configEntry := &configEntry{
		method: cfg.Method,
		pool:   cfg.Pool,
		source: cfg,
	}
	opts, _ := cfg.Meta.GetBag("options")
	configEntry.options = opts

	if err := m.createPool(entry.ID, configEntry); err != nil {
		_ = m.code.DeleteNode(ctx, entry.ID)
		return runtimelua.NewCreatePoolError(err)
	}

	m.storeConfig(entry.ID, configEntry)

	if err := m.registerCaller(ctx, entry.ID, opts); err != nil {
		m.removePool(entry.ID)
		m.deleteConfig(entry.ID)
		_ = m.code.DeleteNode(ctx, entry.ID)
		return err
	}

	m.log.Debug("function added",
		zap.String("id", entry.ID.String()),
		zap.Int("workers", cfg.Pool.Workers),
	)
	return nil
}

// addBytecode adds a bytecode-based function.
func (m *Manager) addBytecode(ctx context.Context, entry registry.Entry) error {
	cfg, err := component.UnpackConfig[api.BytecodeFunctionConfig](ctx, entry)
	if err != nil {
		return runtimelua.NewUnpackConfigError("function", err)
	}

	proto, err := component.LoadAndVerifyBytecode(m.fsRegistry, cfg.FS, cfg.Path, cfg.Hash)
	if err != nil {
		return runtimelua.NewLoadBytecodeError(err)
	}

	node := code.Node{
		ID:     entry.ID,
		Kind:   api.FunctionBytecode,
		Method: cfg.Method,
	}
	imports := component.BuildImports(cfg.Imports, cfg.Modules)
	if err := m.code.AddNodeWithProto(ctx, node, imports, proto); err != nil {
		return runtimelua.NewAddNodeError("function", err)
	}

	configEntry := &configEntry{
		method:   cfg.Method,
		pool:     cfg.Pool,
		bytecode: cfg,
	}
	opts, _ := cfg.Meta.GetBag("options")
	configEntry.options = opts

	if err := m.createPool(entry.ID, configEntry); err != nil {
		_ = m.code.DeleteNode(ctx, entry.ID)
		return runtimelua.NewCreatePoolError(err)
	}

	m.storeConfig(entry.ID, configEntry)

	if err := m.registerCaller(ctx, entry.ID, opts); err != nil {
		m.removePool(entry.ID)
		m.deleteConfig(entry.ID)
		_ = m.code.DeleteNode(ctx, entry.ID)
		return err
	}

	m.log.Debug("bytecode function added",
		zap.String("id", entry.ID.String()),
		zap.String("fs", cfg.FS),
		zap.String("path", cfg.Path),
	)
	return nil
}

// updateSource updates a source-based function.
func (m *Manager) updateSource(ctx context.Context, entry registry.Entry) error {
	cfg, err := component.UnpackConfig[api.FunctionConfig](ctx, entry)
	if err != nil {
		return runtimelua.NewUnpackConfigError("function", err)
	}

	node := code.Node{
		ID:     entry.ID,
		Kind:   api.Function,
		Source: cfg.Source,
		Method: cfg.Method,
	}
	imports := component.BuildImports(cfg.Imports, cfg.Modules)
	if err := m.code.UpdateNode(ctx, node, imports); err != nil {
		return runtimelua.NewUpdateNodeError("function", err)
	}

	configEntry := &configEntry{
		method: cfg.Method,
		pool:   cfg.Pool,
		source: cfg,
	}
	opts, _ := cfg.Meta.GetBag("options")
	configEntry.options = opts

	if err := m.replacePool(entry.ID, configEntry); err != nil {
		return runtimelua.NewReplacePoolError(err)
	}

	m.storeConfig(entry.ID, configEntry)

	if err := m.registerCaller(ctx, entry.ID, opts); err != nil {
		return err
	}

	m.log.Debug("function updated", zap.String("id", entry.ID.String()))
	return nil
}

// updateBytecode updates a bytecode-based function.
func (m *Manager) updateBytecode(ctx context.Context, entry registry.Entry) error {
	cfg, err := component.UnpackConfig[api.BytecodeFunctionConfig](ctx, entry)
	if err != nil {
		return runtimelua.NewUnpackConfigError("function", err)
	}

	proto, err := component.LoadAndVerifyBytecode(m.fsRegistry, cfg.FS, cfg.Path, cfg.Hash)
	if err != nil {
		return runtimelua.NewLoadBytecodeError(err)
	}

	node := code.Node{
		ID:     entry.ID,
		Kind:   api.FunctionBytecode,
		Method: cfg.Method,
	}
	imports := component.BuildImports(cfg.Imports, cfg.Modules)
	if err := m.code.UpdateNodeWithProto(ctx, node, imports, proto); err != nil {
		return runtimelua.NewUpdateNodeError("function", err)
	}

	configEntry := &configEntry{
		method:   cfg.Method,
		pool:     cfg.Pool,
		bytecode: cfg,
	}
	opts, _ := cfg.Meta.GetBag("options")
	configEntry.options = opts

	if err := m.replacePool(entry.ID, configEntry); err != nil {
		return runtimelua.NewReplacePoolError(err)
	}

	m.storeConfig(entry.ID, configEntry)

	if err := m.registerCaller(ctx, entry.ID, opts); err != nil {
		return err
	}

	m.log.Debug("bytecode function updated", zap.String("id", entry.ID.String()))
	return nil
}
