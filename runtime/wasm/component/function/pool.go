// SPDX-License-Identifier: MPL-2.0

package function

import (
	"context"
	"errors"
	"time"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	api "github.com/wippyai/runtime/api/runtime/wasm"
	"github.com/wippyai/runtime/api/supervisor"
	runtimewasm "github.com/wippyai/runtime/runtime/wasm"
	funcpool "github.com/wippyai/runtime/system/scheduler/pool"
	"github.com/wippyai/runtime/system/scheduler/pool/adaptive"
	"github.com/wippyai/runtime/system/scheduler/pool/inline"
	"github.com/wippyai/runtime/system/scheduler/pool/lazy"
	"github.com/wippyai/runtime/system/scheduler/pool/static"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"go.uber.org/zap"
)

func (m *Manager) createPool(id registry.ID, cfg *configEntry, module *wasmrt.Module) error {
	factory := m.processFactory(cfg, module)
	factoryFn := factory.Create()

	execHooks := m.createExecutionHooks()
	var (
		pool funcpool.Pool
		err  error
	)

	if cfg.pool.Type != "" {
		pool, err = m.createPoolByType(cfg.pool.Type, factoryFn, cfg.pool, execHooks)
	} else {
		pool, err = m.autoSelectPool(factoryFn, cfg.pool, execHooks)
	}
	if err != nil {
		return runtimewasm.NewCreatePoolError(err)
	}

	m.mu.Lock()
	m.pools[id] = &poolEntry{
		pool:   pool,
		method: cfg.method,
	}
	started := m.started
	m.mu.Unlock()

	if m.node != nil {
		if err := m.node.RegisterHost(id.String(), pool); err != nil {
			m.log.Warn("failed to register wasm pool as host", zap.String("id", id.String()), zap.Error(err))
		}
	}

	if started {
		pool.Start()
	}

	return nil
}

func (m *Manager) replacePool(id registry.ID, cfg *configEntry, module *wasmrt.Module) error {
	m.removePool(id)
	return m.createPool(id, cfg, module)
}

func (m *Manager) removePool(id registry.ID) {
	m.mu.Lock()
	entry, exists := m.pools[id]
	if exists {
		delete(m.pools, id)
	}
	m.mu.Unlock()

	if exists {
		entry.pool.Stop()
		if m.node != nil {
			m.node.UnregisterHost(id.String())
		}
	}
}

func (m *Manager) autoSelectPool(factory process.FactoryFunc, cfg api.PoolConfig, hooks funcpool.ExecutionHooks) (funcpool.Pool, error) {
	isLazyPool := cfg.Workers == 0 && (cfg.Size == 0 || cfg.MaxSize > 0)
	if isLazyPool {
		maxWorkers := cfg.MaxSize
		if maxWorkers <= 0 {
			maxWorkers = api.DefaultMaxSize
		}
		return lazy.New(factory, m.dispatcher, lazy.Config{
			MaxWorkers:  maxWorkers,
			IdleTimeout: 30 * time.Second,
		}, hooks)
	}

	if cfg.Workers > 0 {
		queueSize := cfg.Buffer
		if queueSize == 0 {
			queueSize = cfg.Workers * 64
		}
		return static.New(factory, m.dispatcher, static.Config{
			Workers:   cfg.Workers,
			QueueSize: queueSize,
		}, hooks)
	}

	return inline.New(factory, m.dispatcher, hooks)
}

func (m *Manager) createPoolByType(poolType string, factory process.FactoryFunc, cfg api.PoolConfig, hooks funcpool.ExecutionHooks) (funcpool.Pool, error) {
	switch poolType {
	case api.PoolTypeInline:
		return inline.New(factory, m.dispatcher, hooks)

	case api.PoolTypeLazy:
		maxWorkers := cfg.MaxSize
		if maxWorkers == 0 {
			maxWorkers = 16
		}
		return lazy.New(factory, m.dispatcher, lazy.Config{
			MaxWorkers:  maxWorkers,
			IdleTimeout: 30 * time.Second,
		}, hooks)

	case api.PoolTypeStatic:
		workers := cfg.Workers
		if workers == 0 {
			workers = cfg.Size
		}
		if workers == 0 {
			workers = 8
		}
		queueSize := cfg.Buffer
		if queueSize == 0 {
			queueSize = workers * 64
		}
		return static.New(factory, m.dispatcher, static.Config{
			Workers:   workers,
			QueueSize: queueSize,
		}, hooks)

	case api.PoolTypeAdaptive:
		maxWorkers := cfg.MaxSize
		if maxWorkers == 0 {
			maxWorkers = 16
		}
		return adaptive.New(factory, m.dispatcher,
			adaptive.WithMaxWorkers(maxWorkers),
			adaptive.WithExecutionHooks(hooks),
			adaptive.WithLogger(m.log),
		)
	default:
		return nil, runtimewasm.NewUnknownPoolTypeError(poolType)
	}
}

func (m *Manager) registerCaller(ctx context.Context, id registry.ID, options runtime.Options) error {
	path := id.String()

	awaitSvc := event.GetAwaitService(ctx)
	if awaitSvc == nil {
		return runtimewasm.NewRegisterCallerError(&id, nil)
	}

	waiter, err := awaitSvc.Prepare(ctx, function.System, "function.(accept|reject)", path, 30*time.Second)
	if err != nil {
		return runtimewasm.NewRegisterCallerError(&id, err)
	}
	defer waiter.Close()

	m.bus.Send(ctx, event.Event{
		System: function.System,
		Kind:   function.FunctionRegister,
		Path:   path,
		Data: &function.FuncEntry{
			Handler: m.Execute,
			Options: options,
		},
	})

	result := waiter.Wait()
	if !result.Accepted {
		return runtimewasm.NewRegisterCallerError(&id, result.Error)
	}
	return nil
}

func (m *Manager) unregisterCaller(ctx context.Context, id registry.ID) {
	m.bus.Send(ctx, event.Event{
		System: function.System,
		Kind:   function.FunctionDelete,
		Path:   id.String(),
	})
}

func (m *Manager) createExecutionHooks() funcpool.ExecutionHooks {
	if m.topo == nil || m.pidReg == nil {
		return funcpool.ExecutionHooks{}
	}

	onStart := func(ctx context.Context, _ process.Process) {
		pid, ok := runtime.GetFramePID(ctx)
		if !ok || pid.String() == "" {
			return
		}

		if err := m.topo.Register(pid); err != nil {
			m.log.Warn("failed to register function PID in topology",
				zap.String("pid", pid.String()),
				zap.Error(err))
		}
	}

	onComplete := func(ctx context.Context, result *runtime.Result) {
		pid, ok := runtime.GetFramePID(ctx)
		if !ok || pid.String() == "" {
			return
		}

		if result.Error != nil && errors.Is(result.Error, supervisor.ErrExit) {
			result.Error = nil
		}

		m.pidReg.Remove(pid)
		m.topo.Complete(pid, result)
	}

	return funcpool.ExecutionHooks{
		OnStart:    onStart,
		OnComplete: onComplete,
	}
}
