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
	api "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/runtime/lua/engine"
	processmod "github.com/wippyai/runtime/runtime/lua/modules/process"
	funcpool "github.com/wippyai/runtime/system/scheduler/pool"
	"go.uber.org/zap"
)

// createPool creates a new pool for a function.
func (m *Manager) createPool(id registry.ID, cfg *configEntry) error {
	factoryFn, err := m.factory.CreateFactory(id, engine.WithModule(processmod.Module))
	if err != nil {
		return api.NewCompileError(err)
	}

	execHooks := m.createExecutionHooks()
	var pool funcpool.Pool

	if cfg.pool.Type != "" {
		pool, err = m.createPoolByType(cfg.pool.Type, factoryFn, cfg.pool, execHooks)
	} else {
		pool, err = m.autoSelectPool(factoryFn, cfg.pool, execHooks)
	}

	if err != nil {
		return api.NewCreatePoolError(err)
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
			m.log.Warn("failed to register pool as host", zap.String("id", id.String()), zap.Error(err))
		}
	}

	if started {
		pool.Start()
	}

	return nil
}

// replacePool stops old pool and creates new one.
func (m *Manager) replacePool(id registry.ID, cfg *configEntry) error {
	m.removePool(id)
	return m.createPool(id, cfg)
}

// removePool stops and removes a pool.
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

// autoSelectPool automatically selects pool type based on config options (legacy behavior).
func (m *Manager) autoSelectPool(factory process.FactoryFunc, cfg api.PoolConfig, hooks funcpool.ExecutionHooks) (funcpool.Pool, error) {
	isLazyPool := cfg.Workers == 0 && (cfg.Size == 0 || cfg.MaxSize > 0)
	if isLazyPool {
		maxWorkers := cfg.MaxSize
		if maxWorkers <= 0 {
			maxWorkers = api.DefaultMaxSize
		}
		return funcpool.NewLazy(factory, m.dispatcher, funcpool.LazyConfig{
			MaxWorkers:  maxWorkers,
			IdleTimeout: 30 * time.Second,
		}, hooks)
	}

	if cfg.Workers > 0 {
		queueSize := cfg.Buffer
		if queueSize == 0 {
			queueSize = cfg.Workers * 64
		}
		return funcpool.NewStatic(factory, m.dispatcher, funcpool.Config{
			Workers:   cfg.Workers,
			QueueSize: queueSize,
		}, hooks)
	}

	return funcpool.NewInline(factory, m.dispatcher, hooks)
}

// createPoolByType creates a pool of the specified type.
func (m *Manager) createPoolByType(poolType string, factory process.FactoryFunc, cfg api.PoolConfig, hooks funcpool.ExecutionHooks) (funcpool.Pool, error) {
	switch poolType {
	case api.PoolTypeInline:
		return funcpool.NewInline(factory, m.dispatcher, hooks)

	case api.PoolTypeLazy:
		maxWorkers := cfg.MaxSize
		if maxWorkers == 0 {
			maxWorkers = 16
		}
		return funcpool.NewLazy(factory, m.dispatcher, funcpool.LazyConfig{
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
		return funcpool.NewStatic(factory, m.dispatcher, funcpool.Config{
			Workers:   workers,
			QueueSize: queueSize,
		}, hooks)

	case api.PoolTypeAdaptive:
		maxWorkers := cfg.MaxSize
		if maxWorkers == 0 {
			maxWorkers = 16
		}
		return funcpool.NewAdaptive(factory, m.dispatcher,
			funcpool.WithMaxWorkers(maxWorkers),
			funcpool.WithExecutionHooks(hooks),
		)

	default:
		return nil, api.NewUnknownPoolTypeError(poolType)
	}
}

// registerCaller registers function in the function system and waits for confirmation.
func (m *Manager) registerCaller(ctx context.Context, id registry.ID, options runtime.Options) error {
	path := id.String()

	waiter, err := m.awaiter.Prepare(ctx, path)
	if err != nil {
		return api.NewRegisterCallerError(id, err)
	}

	m.bus.Send(ctx, event.Event{
		System: function.System,
		Kind:   function.Register,
		Path:   path,
		Data: &function.FuncEntry{
			Handler: m.Execute,
			Options: options,
		},
	})

	result := waiter.Wait()
	if !result.Accepted {
		return api.NewRegisterCallerError(id, result.Error)
	}
	return nil
}

// unregisterCaller removes function from the function system.
func (m *Manager) unregisterCaller(ctx context.Context, id registry.ID) {
	m.bus.Send(ctx, event.Event{
		System: function.System,
		Kind:   function.Delete,
		Path:   id.String(),
	})
}

// createExecutionHooks creates execution hooks for topology integration.
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

		if result.Error != nil {
			if errors.Is(result.Error, supervisor.ErrExit) {
				result.Error = nil
			}
		}

		m.topo.Notify(pid, result)
		m.pidReg.Remove(pid)
		m.topo.Remove(pid)
	}

	return funcpool.ExecutionHooks{
		OnStart:    onStart,
		OnComplete: onComplete,
	}
}
