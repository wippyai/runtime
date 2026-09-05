// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/process"
	api "github.com/wippyai/runtime/api/runtime/wasm"
	runtimewasm "github.com/wippyai/runtime/runtime/wasm"
	wasmcomponent "github.com/wippyai/runtime/runtime/wasm/component"
	wasmengine "github.com/wippyai/runtime/runtime/wasm/engine"
	"github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/actor"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

var (
	// ErrActorFactoryClosed is returned when attempting to spawn a process from an invalidated or closed factory.
	ErrActorFactoryClosed = errors.New("wasm actor factory is closed")
)

// ActorFactory produces isolated ActorProcess instances for a verified WASM actor module.
type ActorFactory struct {
	bytes        []byte
	isComponent  bool
	cfg          *api.ProcessConfig
	hostRegistry *wasmcomponent.HostRegistry
	fsRegistry   fsapi.Registry
	memoryPages  uint32
	mu           sync.RWMutex
	closed       atomic.Bool
}

// NewActorFactory creates a new actor factory with frozen verified bytes.
func NewActorFactory(
	bytes []byte,
	isComponent bool,
	cfg *api.ProcessConfig,
	hostRegistry *wasmcomponent.HostRegistry,
	fsRegistry fsapi.Registry,
) *ActorFactory {
	memBytes := cfg.Limits().EffectiveMemoryBytes()
	pages := uint32(memBytes / api.MinProcessMemoryBytesMultiple)
	return &ActorFactory{
		bytes:        append([]byte(nil), bytes...),
		isComponent:  isComponent,
		cfg:          cfg,
		hostRegistry: hostRegistry,
		fsRegistry:   fsRegistry,
		memoryPages:  pages,
	}
}

// Close invalidates the factory, preventing subsequent spawns.
func (f *ActorFactory) Close() {
	f.mu.Lock()
	f.closed.Store(true)
	f.mu.Unlock()
}

// IsClosed reports whether the factory has been invalidated or closed.
func (f *ActorFactory) IsClosed() bool {
	return f.closed.Load()
}

// Create returns a process.FactoryFunc for process spawning.
// Every PID gets a fresh backend runtime with wasmrt.NewWithConfig, a fresh
// HostRegistry.Fork(), frozen verified component bytes, and owns module and resource table lifetimes.
func (f *ActorFactory) Create() process.FactoryFunc {
	return func() (process.Process, error) {
		if f.closed.Load() {
			return nil, ErrActorFactoryClosed
		}

		// Use background context so future spawns do not capture registration or request deadlines.
		spawnCtx := context.Background()
		rtCfg := &wasmrt.Config{
			MemoryLimitPages:   f.memoryPages,
			CloseOnContextDone: true,
		}
		rt, err := wasmrt.NewWithConfig(spawnCtx, rtCfg)
		if err != nil {
			return nil, err
		}

		forkedHosts := f.hostRegistry.Fork()
		// Bound host handles independently of guest linear memory.
		forkedHosts.SetSharedResources(preview2.NewResourceTableWithLimits(4096, f.cfg.Limits().EffectiveMaxOpenSockets()))

		if err := forkedHosts.EnsureImports(spawnCtx, rt, f.cfg.Imports, f.isComponent); err != nil {
			forkedHosts.CloseResources()
			_ = rt.Close(spawnCtx)
			return nil, err
		}

		var mod *wasmrt.Module
		if f.isComponent {
			mod, err = rt.LoadComponent(spawnCtx, f.bytes)
		} else {
			mod, err = rt.LoadWASM(spawnCtx, f.bytes, f.cfg.WIT)
		}
		if err != nil {
			forkedHosts.CloseResources()
			_ = rt.Close(spawnCtx)
			return nil, runtimewasm.NewLoadWASMError(err)
		}

		if err := mod.Compile(spawnCtx); err != nil {
			forkedHosts.CloseResources()
			_ = rt.Close(spawnCtx)
			return nil, runtimewasm.NewCompileModuleError(err)
		}

		proc := wasmengine.NewProcess(
			mod,
			f.cfg.EffectiveTransport(),
			f.cfg.WASI,
			f.cfg.EffectiveLimitsConfig(),
			f.fsRegistry,
		)

		cleanup := func() {
			forkedHosts.CloseResources()
			_ = rt.Close(context.Background())
		}

		mb := f.cfg.Mailbox()
		actorLimits := actor.Limits{
			Capacity:     mb.EffectiveCapacity(),
			Bytes:        mb.EffectiveBytes(),
			MessageBytes: mb.EffectiveMessageBytes(),
		}

		actorProc := wasmengine.NewActorProcess(proc, actorLimits, cleanup)

		f.mu.RLock()
		isClosed := f.closed.Load()
		f.mu.RUnlock()
		if isClosed {
			actorProc.Close()
			return nil, ErrActorFactoryClosed
		}

		return actorProc, nil
	}
}
