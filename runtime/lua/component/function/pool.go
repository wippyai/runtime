// SPDX-License-Identifier: MPL-2.0

package function

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	api "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/api/supervisor"
	runtimelua "github.com/wippyai/runtime/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/engine"
	processmod "github.com/wippyai/runtime/runtime/lua/modules/process"
	funcpool "github.com/wippyai/runtime/system/scheduler/pool"
	"github.com/wippyai/runtime/system/scheduler/pool/adaptive"
	"github.com/wippyai/runtime/system/scheduler/pool/inline"
	"github.com/wippyai/runtime/system/scheduler/pool/lazy"
	"github.com/wippyai/runtime/system/scheduler/pool/static"
	"go.uber.org/zap"
)

func newPoolEntry(pool funcpool.Pool, method, hostID string) *poolEntry {
	return &poolEntry{
		pool:    pool,
		method:  method,
		hostID:  hostID,
		drained: make(chan struct{}),
	}
}

func (e *poolEntry) acquire() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.retired {
		return false
	}
	e.active++
	return true
}

func (e *poolEntry) release() {
	e.mu.Lock()
	e.active--
	if e.retired && e.active == 0 {
		e.stopOnce.Do(func() { close(e.drained) })
	}
	e.mu.Unlock()
}

func (e *poolEntry) retire(stop func()) {
	e.mu.Lock()
	if e.retired {
		e.mu.Unlock()
		return
	}
	e.retired = true
	if e.active == 0 {
		e.stopOnce.Do(func() { close(e.drained) })
		e.mu.Unlock()
		stop()
		return
	}
	drained := e.drained
	e.mu.Unlock()

	go func() {
		<-drained
		stop()
	}()
}

// createPool creates a new pool for a function.
func (m *Manager) createPool(id registry.ID, cfg *configEntry) error {
	pool, err := m.buildPool(id, cfg)
	if err != nil {
		return err
	}

	hostID := m.nextPoolHostID(id)

	m.mu.RLock()
	started := m.started
	m.mu.RUnlock()
	if started {
		pool.Start()
	}

	if started && m.node != nil {
		if err := m.node.RegisterHost(hostID, pool); err != nil {
			pool.Stop()
			return err
		}
	}

	m.mu.Lock()
	m.pools[id] = newPoolEntry(pool, cfg.method, hostID)
	m.mu.Unlock()

	return nil
}

func (m *Manager) buildPool(id registry.ID, cfg *configEntry) (funcpool.Pool, error) {
	factoryFn, err := m.factory.CreateFactory(id, engine.WithModule(processmod.Module))
	if err != nil {
		return nil, err // Already has compile context from code.Manager
	}

	execHooks := m.createExecutionHooks()
	var pool funcpool.Pool

	if cfg.pool.Type != "" {
		pool, err = m.createPoolByType(cfg.pool.Type, factoryFn, cfg.pool, execHooks)
	} else {
		pool, err = m.autoSelectPool(factoryFn, cfg.pool, execHooks)
	}

	if err != nil {
		return nil, runtimelua.NewCreatePoolError(err)
	}

	return pool, nil
}

// replacePool creates and starts the replacement before making it visible.
func (m *Manager) replacePool(id registry.ID, cfg *configEntry) error {
	pool, err := m.buildPool(id, cfg)
	if err != nil {
		return err
	}

	hostID := m.nextPoolHostID(id)

	m.mu.RLock()
	started := m.started
	m.mu.RUnlock()
	if started {
		pool.Start()
	}

	if started && m.node != nil {
		if err := m.node.RegisterHost(hostID, pool); err != nil {
			pool.Stop()
			return err
		}
	}

	newEntry := newPoolEntry(pool, cfg.method, hostID)
	m.mu.Lock()
	oldEntry, exists := m.pools[id]
	m.pools[id] = newEntry
	m.mu.Unlock()

	if exists {
		m.retirePoolEntry(oldEntry)
	}
	return nil
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
		m.retirePoolEntry(entry)
	}
}

func (m *Manager) retirePoolEntry(entry *poolEntry) {
	if entry == nil {
		return
	}
	entry.retire(func() {
		entry.pool.Stop()
		if m.node != nil {
			m.node.UnregisterHost(entry.hostID)
		}
	})
}

func (m *Manager) nextPoolHostID(id registry.ID) string {
	return id.String() + "#lua." + strconv.FormatUint(m.hostSeq.Add(1), 10)
}

// autoSelectPool automatically selects pool type based on config options (legacy behavior).
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

// createPoolByType creates a pool of the specified type.
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
		return nil, runtimelua.NewUnknownPoolTypeError(poolType)
	}
}

// registerCaller registers function in the function system and waits for confirmation.
func (m *Manager) registerCaller(ctx context.Context, id registry.ID, options runtime.Options) error {
	path := id.String()

	awaitSvc := event.GetAwaitService(ctx)
	if awaitSvc == nil {
		return runtimelua.NewRegisterCallerError(&id, nil)
	}

	waiter, err := awaitSvc.Prepare(ctx, function.System, "function.(accept|reject)", path, event.DefaultAwaitTimeout)
	if err != nil {
		return runtimelua.NewRegisterCallerError(&id, err)
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
		return runtimelua.NewRegisterCallerError(&id, result.Error)
	}
	return nil
}

// unregisterCaller removes function from the function system.
func (m *Manager) unregisterCaller(ctx context.Context, id registry.ID) {
	m.bus.Send(ctx, event.Event{
		System: function.System,
		Kind:   function.FunctionDelete,
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

		m.pidReg.Remove(pid)
		m.topo.Complete(pid, result)
	}

	return funcpool.ExecutionHooks{
		OnStart:    onStart,
		OnComplete: onComplete,
	}
}
