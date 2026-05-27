// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"

	"github.com/wippyai/runtime/api/event"
	processapi "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/runtime/wasm"
	runtimewasm "github.com/wippyai/runtime/runtime/wasm"
	wasmcomponent "github.com/wippyai/runtime/runtime/wasm/component"
	wasmengine "github.com/wippyai/runtime/runtime/wasm/engine"
	wasmlib "github.com/wippyai/wasm-runtime/component"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"go.uber.org/zap"
)

func (m *Manager) Add(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case api.ProcessWASM:
		return m.addWASM(ctx, entry)
	default:
		return runtimewasm.NewInvalidEntryKindError(entry.Kind, api.ProcessWASM)
	}
}

func (m *Manager) Update(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case api.ProcessWASM:
		return m.updateWASM(ctx, entry)
	default:
		return runtimewasm.NewInvalidEntryKindError(entry.Kind, api.ProcessWASM)
	}
}

func (m *Manager) Delete(ctx context.Context, entry registry.Entry) error {
	switch entry.Kind {
	case api.ProcessWASM:
		m.deleteConfig(entry.ID)
		m.unregisterFactory(ctx, entry.ID)
		m.log.Debug("wasm process deleted", zap.String("id", entry.ID.String()))
		return nil
	default:
		return runtimewasm.NewInvalidEntryKindError(entry.Kind, api.ProcessWASM)
	}
}

// Invalidate reloads configured wasm process modules.
func (m *Manager) Invalidate(ctx context.Context, ids []registry.ID) {
	for _, id := range ids {
		cfg := m.getConfig(id)
		if cfg == nil || cfg.wasm == nil {
			continue
		}

		module, err := m.loadWASMModule(ctx, cfg.wasm)
		if err != nil {
			m.log.Error("failed to reload wasm process module",
				zap.String("id", id.String()),
				zap.Error(err),
			)
			continue
		}

		if err := m.registerFactory(ctx, id, cfg, module); err != nil {
			m.log.Error("failed to reregister wasm process factory",
				zap.String("id", id.String()),
				zap.Error(err),
			)
		}
	}
}

func (m *Manager) addWASM(ctx context.Context, entry registry.Entry) error {
	cfg, err := wasmcomponent.UnpackConfig[api.FunctionConfig](ctx, entry)
	if err != nil {
		return runtimewasm.NewUnpackConfigError("process.wasm", err)
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
		limits:    cfg.Limits,
	}
	m.storeConfig(entry.ID, ce)

	if err := m.registerFactory(ctx, entry.ID, ce, module); err != nil {
		m.deleteConfig(entry.ID)
		return err
	}

	m.log.Debug("wasm process added",
		zap.String("id", entry.ID.String()),
		zap.String("method", cfg.Method),
		zap.String("fs", cfg.FS),
		zap.String("path", cfg.Path),
	)
	return nil
}

func (m *Manager) updateWASM(ctx context.Context, entry registry.Entry) error {
	cfg, err := wasmcomponent.UnpackConfig[api.FunctionConfig](ctx, entry)
	if err != nil {
		return runtimewasm.NewUnpackConfigError("process.wasm", err)
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
		limits:    cfg.Limits,
	}
	m.storeConfig(entry.ID, ce)

	if err := m.registerFactory(ctx, entry.ID, ce, module); err != nil {
		return err
	}

	m.log.Debug("wasm process updated", zap.String("id", entry.ID.String()))
	return nil
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

func (m *Manager) registerFactory(ctx context.Context, id registry.ID, cfg *configEntry, module *wasmrt.Module) error {
	awaitSvc := event.GetAwaitService(ctx)
	if awaitSvc == nil {
		return runtimewasm.NewRegisterProcessFactoryError(&id, nil)
	}

	factory := wasmengine.NewFactory(module, cfg.transport, cfg.wasi, cfg.limits, m.fsRegistry).Create()
	method := cfg.method
	if method == "" {
		method = "main"
	}

	path := id.String()
	waiter, err := awaitSvc.Prepare(ctx, processapi.System, "factory.(accept|reject)", path, event.DefaultAwaitTimeout)
	if err != nil {
		return runtimewasm.NewRegisterProcessFactoryError(&id, err)
	}
	defer waiter.Close()

	m.bus.Send(ctx, event.Event{
		System: processapi.System,
		Kind:   processapi.FactoryRegister,
		Path:   path,
		Data: &processapi.FactoryEntry{
			Factory: factory,
			Meta: processapi.Meta{
				Method: method,
			},
		},
	})

	result := waiter.Wait()
	if !result.Accepted {
		return runtimewasm.NewRegisterProcessFactoryError(&id, result.Error)
	}
	return nil
}

func (m *Manager) unregisterFactory(ctx context.Context, id registry.ID) {
	m.bus.Send(ctx, event.Event{
		System: processapi.System,
		Kind:   processapi.FactoryDelete,
		Path:   id.String(),
	})
}
