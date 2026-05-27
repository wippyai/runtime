// SPDX-License-Identifier: MPL-2.0

package function

import (
	"context"
	"fmt"

	ctxapi "github.com/wippyai/runtime/api/context"
	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	api "github.com/wippyai/runtime/api/runtime/wasm"
	runtimewasm "github.com/wippyai/runtime/runtime/wasm"
	wasmcomponent "github.com/wippyai/runtime/runtime/wasm/component"
	wasmlib "github.com/wippyai/wasm-runtime/component"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"go.uber.org/zap"
)

func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case api.FunctionWAT:
		return m.addWAT(ctx, entry)
	case api.FunctionWASM:
		return m.addWASM(ctx, entry)
	default:
		return runtimewasm.NewInvalidEntryKindError(entry.Kind, api.FunctionWAT, api.FunctionWASM)
	}
}

func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case api.FunctionWAT:
		return m.updateWAT(ctx, entry)
	case api.FunctionWASM:
		return m.updateWASM(ctx, entry)
	default:
		return runtimewasm.NewInvalidEntryKindError(entry.Kind, api.FunctionWAT, api.FunctionWASM)
	}
}

func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case api.FunctionWAT, api.FunctionWASM:
		m.removePool(entry.ID)
		m.deleteConfig(entry.ID)
		m.unregisterCaller(ctx, entry.ID)
		m.log.Debug("wasm function deleted", zap.String("id", entry.ID.String()))
		return nil
	default:
		return runtimewasm.NewInvalidEntryKindError(entry.Kind, api.FunctionWAT, api.FunctionWASM)
	}
}

// Invalidate reloads configured wasm functions.
func (m *Manager) Invalidate(ctx context.Context, ids []registry.ID) {
	_ = ctx
	_ = ids
	// No-op for v1: WASM functions have no code graph/module dependency invalidation path.
	// Use registry entry updates to reload code.
}

// Execute runs a wasm function by task.
func (m *Manager) Execute(ctx context.Context, task runtime.Task) (*runtime.Result, error) {
	m.mu.RLock()
	entry, exists := m.pools[task.ID]
	if exists && !entry.acquire() {
		exists = false
	}
	m.mu.RUnlock()

	if !exists {
		return nil, runtimewasm.NewPoolNotFoundError(task.ID.String())
	}
	defer entry.release()

	var err error
	if task.Context, err = netapi.ApplyOverlayPair(ctx, task.Options, task.Context); err != nil {
		return nil, err
	}

	if len(task.Context) > 0 {
		fc := ctxapi.FrameFromContext(ctx)
		if fc != nil {
			if err := fc.SetMultiple(task.Context...); err != nil {
				return nil, fmt.Errorf("set task context: %w", err)
			}
		}
	}

	if entry.hostID != "" {
		if framePID, ok := runtime.GetFramePID(ctx); ok {
			framePID.Host = entry.hostID
			if err := runtime.SetFramePID(ctx, framePID.Precomputed()); err != nil {
				return nil, fmt.Errorf("set frame pid: %w", err)
			}
		}
	}

	return entry.pool.Call(ctx, entry.method, task.Payloads)
}

func (m *Manager) addWAT(ctx context.Context, entry registry.Entry) error {
	cfg, err := wasmcomponent.UnpackConfig[api.WATFunctionConfig](ctx, entry)
	if err != nil {
		return runtimewasm.NewUnpackConfigError("function.wat", err)
	}

	module, err := m.loadWATModule(ctx, cfg)
	if err != nil {
		return err
	}

	ce := &configEntry{
		wat:       cfg,
		method:    cfg.Method,
		transport: cfg.EffectiveTransport(),
		wasi:      cfg.WASI,
		pool:      cfg.Pool,
		limits:    cfg.Limits,
		kind:      api.FunctionWAT,
	}
	opts, _ := cfg.Meta.GetBag("options")
	ce.options = opts

	if err := m.createPool(entry.ID, ce, module); err != nil {
		return err
	}
	m.storeConfig(entry.ID, ce)

	if err := m.registerCaller(ctx, entry.ID, opts); err != nil {
		m.removePool(entry.ID)
		m.deleteConfig(entry.ID)
		return err
	}

	m.log.Debug("wasm wat function added",
		zap.String("id", entry.ID.String()),
		zap.String("method", cfg.Method),
	)
	return nil
}

func (m *Manager) addWASM(ctx context.Context, entry registry.Entry) error {
	cfg, err := wasmcomponent.UnpackConfig[api.FunctionConfig](ctx, entry)
	if err != nil {
		return runtimewasm.NewUnpackConfigError("function.wasm", err)
	}

	module, err := m.loadWASMModule(ctx, cfg)
	if err != nil {
		return err
	}

	ce := &configEntry{
		wasm:      cfg,
		method:    cfg.Method,
		transport: cfg.EffectiveTransport(),
		wasi:      cfg.WASI,
		pool:      cfg.Pool,
		limits:    cfg.Limits,
		kind:      api.FunctionWASM,
	}
	opts, _ := cfg.Meta.GetBag("options")
	ce.options = opts

	if err := m.createPool(entry.ID, ce, module); err != nil {
		return err
	}
	m.storeConfig(entry.ID, ce)

	if err := m.registerCaller(ctx, entry.ID, opts); err != nil {
		m.removePool(entry.ID)
		m.deleteConfig(entry.ID)
		return err
	}

	m.log.Debug("wasm function added",
		zap.String("id", entry.ID.String()),
		zap.String("method", cfg.Method),
		zap.String("fs", cfg.FS),
		zap.String("path", cfg.Path),
	)
	return nil
}

func (m *Manager) updateWAT(ctx context.Context, entry registry.Entry) error {
	cfg, err := wasmcomponent.UnpackConfig[api.WATFunctionConfig](ctx, entry)
	if err != nil {
		return runtimewasm.NewUnpackConfigError("function.wat", err)
	}

	module, err := m.loadWATModule(ctx, cfg)
	if err != nil {
		return err
	}

	ce := &configEntry{
		wat:       cfg,
		method:    cfg.Method,
		transport: cfg.EffectiveTransport(),
		wasi:      cfg.WASI,
		pool:      cfg.Pool,
		limits:    cfg.Limits,
		kind:      api.FunctionWAT,
	}
	opts, _ := cfg.Meta.GetBag("options")
	ce.options = opts

	if err := m.replacePool(entry.ID, ce, module); err != nil {
		return runtimewasm.NewReplacePoolError(err)
	}
	m.storeConfig(entry.ID, ce)

	if err := m.registerCaller(ctx, entry.ID, opts); err != nil {
		return err
	}

	m.log.Debug("wasm wat function updated", zap.String("id", entry.ID.String()))
	return nil
}

func (m *Manager) updateWASM(ctx context.Context, entry registry.Entry) error {
	cfg, err := wasmcomponent.UnpackConfig[api.FunctionConfig](ctx, entry)
	if err != nil {
		return runtimewasm.NewUnpackConfigError("function.wasm", err)
	}

	module, err := m.loadWASMModule(ctx, cfg)
	if err != nil {
		return err
	}

	ce := &configEntry{
		wasm:      cfg,
		method:    cfg.Method,
		transport: cfg.EffectiveTransport(),
		wasi:      cfg.WASI,
		pool:      cfg.Pool,
		limits:    cfg.Limits,
		kind:      api.FunctionWASM,
	}
	opts, _ := cfg.Meta.GetBag("options")
	ce.options = opts

	if err := m.replacePool(entry.ID, ce, module); err != nil {
		return runtimewasm.NewReplacePoolError(err)
	}
	m.storeConfig(entry.ID, ce)

	if err := m.registerCaller(ctx, entry.ID, opts); err != nil {
		return err
	}

	m.log.Debug("wasm function updated", zap.String("id", entry.ID.String()))
	return nil
}

func (m *Manager) loadModule(ctx context.Context, cfg *configEntry) (*wasmrt.Module, error) {
	switch cfg.kind {
	case api.FunctionWAT:
		return m.loadWATModule(ctx, cfg.wat)
	case api.FunctionWASM:
		return m.loadWASMModule(ctx, cfg.wasm)
	default:
		return nil, runtimewasm.NewInvalidEntryKindError(cfg.kind, api.FunctionWAT, api.FunctionWASM)
	}
}

func (m *Manager) loadWATModule(ctx context.Context, cfg *api.WATFunctionConfig) (*wasmrt.Module, error) {
	if err := m.ensureImportHosts(ctx, cfg.Imports, false); err != nil {
		return nil, err
	}

	rt := m.runtimeInstance(false)
	if rt == nil {
		return nil, runtimewasm.ErrRuntimeNotStarted
	}

	module, err := rt.LoadWAT(ctx, cfg.Source, cfg.WIT)
	if err != nil {
		return nil, runtimewasm.NewLoadWATError(err)
	}
	if err := module.Compile(ctx); err != nil {
		return nil, runtimewasm.NewCompileModuleError(err)
	}
	return module, nil
}

func (m *Manager) loadWASMModule(ctx context.Context, cfg *api.FunctionConfig) (*wasmrt.Module, error) {
	data, err := wasmcomponent.LoadAndVerifyWASM(m.fsRegistry, cfg.FS, cfg.Path, cfg.Hash)
	if err != nil {
		return nil, err
	}

	isComponent := wasmlib.IsComponent(data)
	if err := m.ensureImportHosts(ctx, cfg.Imports, isComponent); err != nil {
		return nil, err
	}

	rt := m.runtimeInstance(isComponent)
	if rt == nil {
		return nil, runtimewasm.ErrRuntimeNotStarted
	}

	var module *wasmrt.Module
	if isComponent {
		module, err = rt.LoadComponent(ctx, data)
	} else {
		module, err = rt.LoadWASM(ctx, data, cfg.WIT)
	}
	if err != nil {
		return nil, runtimewasm.NewLoadWASMError(err)
	}

	if err := module.Compile(ctx); err != nil {
		return nil, runtimewasm.NewCompileModuleError(err)
	}
	return module, nil
}
