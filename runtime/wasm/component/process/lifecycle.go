// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/event"
	processapi "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/runtime/wasm"
	"github.com/wippyai/runtime/api/security"
	runtimewasm "github.com/wippyai/runtime/runtime/wasm"
	wasmcomponent "github.com/wippyai/runtime/runtime/wasm/component"
	entrycfg "github.com/wippyai/runtime/system/entry"
	wasmlib "github.com/wippyai/wasm-runtime/component"
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
		m.opMu.Lock()
		defer m.opMu.Unlock()

		old := m.deleteConfig(entry.ID)
		if old != nil && old.factory != nil {
			old.factory.Close()
		}
		m.unregisterFactory(ctx, entry.ID)
		m.log.Debug("wasm process deleted", zap.String("id", entry.ID.String()))
		return nil
	default:
		return runtimewasm.NewInvalidEntryKindError(entry.Kind, api.ProcessWASM)
	}
}

// Invalidate reloads configured wasm process modules.
func (m *Manager) Invalidate(ctx context.Context, ids []registry.ID) {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	if !m.isStarted() {
		return
	}

	for _, id := range ids {
		old := m.getConfig(id)
		if old == nil || old.cfg == nil {
			continue
		}

		data, err := wasmcomponent.LoadAndVerifyWASM(m.fsRegistry, old.cfg.FS, old.cfg.Path, old.cfg.Hash)
		if err != nil {
			m.log.Error("failed to reload wasm process module",
				zap.String("id", id.String()),
				zap.Error(err),
			)
			continue
		}

		frozenBytes := append([]byte(nil), data...)
		isComponent := wasmlib.IsComponent(frozenBytes)

		newFactory := NewActorFactory(frozenBytes, isComponent, old.cfg, m.hostRegistry, m.fsRegistry)
		if err := newFactory.warm(ctx); err != nil {
			newFactory.Close()
			m.log.Error("failed to validate reloaded wasm process module",
				zap.String("id", id.String()),
				zap.Error(err),
			)
			continue
		}

		method := old.cfg.Method
		if method == "" {
			method = "run"
		}

		if err := m.registerFactory(ctx, id, method, old.security, old.cfg.WorkerClass(), newFactory.Create()); err != nil {
			newFactory.Close()
			m.log.Error("failed to reregister wasm process factory",
				zap.String("id", id.String()),
				zap.Error(err),
			)
			continue
		}

		m.storeConfig(id, &configEntry{
			cfg:         old.cfg,
			bytes:       frozenBytes,
			isComponent: isComponent,
			factory:     newFactory,
			security:    old.security,
		})
		if old.factory != nil {
			old.factory.Close()
		}
	}
}

func (m *Manager) addWASM(ctx context.Context, entry registry.Entry) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	if !m.isStarted() {
		return runtimewasm.ErrRuntimeNotStarted
	}

	cfg, err := entrycfg.DecodeEntryConfigFromContext[api.ProcessConfig](ctx, entry)
	if err != nil {
		return runtimewasm.NewUnpackConfigError("process.wasm", err)
	}

	if cfg.WorkerClass() != api.WorkerClassWASM {
		return runtimewasm.NewValidationError(fmt.Errorf("unsupported worker_class %q, only %q is supported", cfg.WorkerClass(), api.WorkerClassWASM))
	}

	data, err := wasmcomponent.LoadAndVerifyWASM(m.fsRegistry, cfg.FS, cfg.Path, cfg.Hash)
	if err != nil {
		return err
	}

	frozenBytes := append([]byte(nil), data...)
	isComponent := wasmlib.IsComponent(frozenBytes)

	factory := NewActorFactory(frozenBytes, isComponent, cfg, m.hostRegistry, m.fsRegistry)
	if err := factory.warm(ctx); err != nil {
		factory.Close()
		return err
	}

	method := cfg.Method
	if method == "" {
		method = "run"
	}

	if err := m.registerFactory(ctx, entry.ID, method, cfg.Security, cfg.WorkerClass(), factory.Create()); err != nil {
		factory.Close()
		return err
	}

	m.storeConfig(entry.ID, &configEntry{
		cfg:         cfg,
		bytes:       frozenBytes,
		isComponent: isComponent,
		factory:     factory,
		security:    cfg.Security,
	})

	wasmcomponent.LogOptionDeprecations(m.log, entry.ID, cfg)

	m.log.Debug("wasm process added",
		zap.String("id", entry.ID.String()),
		zap.String("method", cfg.Method),
		zap.String("fs", cfg.FS),
		zap.String("path", cfg.Path),
	)
	return nil
}

func (m *Manager) updateWASM(ctx context.Context, entry registry.Entry) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	if !m.isStarted() {
		return runtimewasm.ErrRuntimeNotStarted
	}

	cfg, err := entrycfg.DecodeEntryConfigFromContext[api.ProcessConfig](ctx, entry)
	if err != nil {
		return runtimewasm.NewUnpackConfigError("process.wasm", err)
	}

	if cfg.WorkerClass() != api.WorkerClassWASM {
		return runtimewasm.NewValidationError(fmt.Errorf("unsupported worker_class %q, only %q is supported", cfg.WorkerClass(), api.WorkerClassWASM))
	}

	data, err := wasmcomponent.LoadAndVerifyWASM(m.fsRegistry, cfg.FS, cfg.Path, cfg.Hash)
	if err != nil {
		return err
	}

	frozenBytes := append([]byte(nil), data...)
	isComponent := wasmlib.IsComponent(frozenBytes)

	newFactory := NewActorFactory(frozenBytes, isComponent, cfg, m.hostRegistry, m.fsRegistry)
	if err := newFactory.warm(ctx); err != nil {
		newFactory.Close()
		return err
	}

	method := cfg.Method
	if method == "" {
		method = "run"
	}

	if err := m.registerFactory(ctx, entry.ID, method, cfg.Security, cfg.WorkerClass(), newFactory.Create()); err != nil {
		newFactory.Close()
		return err
	}

	old := m.getConfig(entry.ID)
	m.storeConfig(entry.ID, &configEntry{
		cfg:         cfg,
		bytes:       frozenBytes,
		isComponent: isComponent,
		factory:     newFactory,
		security:    cfg.Security,
	})
	if old != nil && old.factory != nil {
		old.factory.Close()
	}

	wasmcomponent.LogOptionDeprecations(m.log, entry.ID, cfg)

	m.log.Debug("wasm process updated", zap.String("id", entry.ID.String()))
	return nil
}

func (m *Manager) registerFactory(ctx context.Context, id registry.ID, method string, sec *security.Config, workerClass string, factory processapi.FactoryFunc) error {
	awaitSvc := event.GetAwaitService(ctx)
	if awaitSvc == nil {
		return runtimewasm.NewRegisterProcessFactoryError(&id, nil)
	}

	if method == "" {
		method = "run"
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
				Method:      method,
				Security:    sec,
				WorkerClass: workerClass,
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
