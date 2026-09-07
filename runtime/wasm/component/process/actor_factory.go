// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/tetratelabs/wazero"
	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/process"
	api "github.com/wippyai/runtime/api/runtime/wasm"
	runtimewasm "github.com/wippyai/runtime/runtime/wasm"
	wasmcomponent "github.com/wippyai/runtime/runtime/wasm/component"
	wasmengine "github.com/wippyai/runtime/runtime/wasm/engine"
	"github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/actor"
	memorybudget "github.com/wippyai/wasm-runtime/memory/budget"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

var (
	// ErrActorFactoryClosed is returned when attempting to spawn a process from an invalidated or closed factory.
	ErrActorFactoryClosed = errors.New("wasm actor factory is closed")
)

// factoryGeneration owns one in-memory compilation cache for a factory lifetime.
// refs counts the factory owner plus in-progress warm/spawns and live actors.
// Cache.Close runs when the last ref is released, after those runtimes have closed.
type factoryGeneration struct {
	cache       wazero.CompilationCache
	refs        atomic.Int32
	cacheClosed atomic.Bool
}

func newFactoryGeneration() *factoryGeneration {
	g := &factoryGeneration{cache: wazero.NewCompilationCache()}
	g.refs.Store(1)
	return g
}

func (g *factoryGeneration) retain() {
	g.refs.Add(1)
}

func (g *factoryGeneration) release() {
	if g.refs.Add(-1) != 0 {
		return
	}
	if g.cacheClosed.CompareAndSwap(false, true) && g.cache != nil {
		_ = g.cache.Close(context.Background())
	}
}

// ActorFactory produces isolated ActorProcess instances for a verified WASM actor module.
//
// Compiled guest code is shared through gen.cache. A pin runtime holds compiled
// module handles without guest instances, keeping reusable code available while
// the factory is open. Invalidation closes the pin; live actors keep their own
// compiled handles until they exit.
type ActorFactory struct {
	fsRegistry      fsapi.Registry
	pinErr          error
	pinHosts        *wasmcomponent.HostRegistry
	cfg             *api.ProcessConfig
	hostRegistry    *wasmcomponent.HostRegistry
	gen             *factoryGeneration
	pinRT           *wasmrt.Runtime
	bytes           []byte
	hostBufferBytes int64
	pinOnce         sync.Once
	pinMu           sync.Mutex
	mu              sync.Mutex
	memoryPages     uint32
	closed          atomic.Bool
	isComponent     bool
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
		bytes:           append([]byte(nil), bytes...),
		isComponent:     isComponent,
		cfg:             cfg,
		hostRegistry:    hostRegistry,
		fsRegistry:      fsRegistry,
		memoryPages:     pages,
		hostBufferBytes: cfg.Limits().HostBufferBytes,
		gen:             newFactoryGeneration(),
	}
}

func (f *ActorFactory) runtimeConfig() *wasmrt.Config {
	return &wasmrt.Config{
		CompilationCache:   f.gen.cache,
		MemoryLimitPages:   f.memoryPages,
		CloseOnContextDone: true,
	}
}

// Close invalidates the factory, preventing subsequent spawns.
// The pin runtime is released here; the compilation cache stays until the last
// in-progress warm, spawn, or live actor releases the generation.
func (f *ActorFactory) Close() {
	f.mu.Lock()
	if f.closed.Load() {
		f.mu.Unlock()
		return
	}
	f.closed.Store(true)
	f.mu.Unlock()

	f.closePin()
	f.gen.release()
}

// IsClosed reports whether the factory has been invalidated or closed.
func (f *ActorFactory) IsClosed() bool {
	return f.closed.Load()
}

func (f *ActorFactory) tryRetain() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed.Load() {
		return false
	}
	f.gen.retain()
	return true
}

func (f *ActorFactory) closePin() {
	f.pinMu.Lock()
	rt := f.pinRT
	hosts := f.pinHosts
	f.pinRT = nil
	f.pinHosts = nil
	f.pinMu.Unlock()

	closePinnedRuntime(hosts, rt)
}

func closePinnedRuntime(hosts *wasmcomponent.HostRegistry, rt *wasmrt.Runtime) {
	if hosts != nil {
		hosts.CloseResources()
	}
	if rt != nil {
		_ = rt.Close(context.Background())
	}
}

// warm compiles guest code onto the factory pin runtime so later spawns hit the cache.
func (f *ActorFactory) warm(ctx context.Context) error {
	if !f.tryRetain() {
		return ErrActorFactoryClosed
	}
	defer f.gen.release()
	return f.ensurePin(ctx)
}

func (f *ActorFactory) ensurePin(ctx context.Context) error {
	if f.closed.Load() {
		return ErrActorFactoryClosed
	}
	f.pinOnce.Do(func() {
		f.pinErr = f.initPin(ctx)
	})
	if f.closed.Load() {
		return ErrActorFactoryClosed
	}
	return f.pinErr
}

func (f *ActorFactory) initPin(ctx context.Context) error {
	rt, err := wasmrt.NewWithConfig(context.Background(), f.runtimeConfig())
	if err != nil {
		return err
	}
	hosts := f.hostRegistry.Fork()
	if err := hosts.EnsureImports(ctx, rt, f.cfg.Imports, f.isComponent); err != nil {
		closePinnedRuntime(hosts, rt)
		return err
	}
	if _, err := f.loadAndCompile(ctx, rt); err != nil {
		closePinnedRuntime(hosts, rt)
		return err
	}

	f.pinMu.Lock()
	defer f.pinMu.Unlock()
	if f.closed.Load() {
		closePinnedRuntime(hosts, rt)
		return ErrActorFactoryClosed
	}
	f.pinRT = rt
	f.pinHosts = hosts
	return nil
}

func (f *ActorFactory) loadAndCompile(ctx context.Context, rt *wasmrt.Runtime) (*wasmrt.Module, error) {
	var mod *wasmrt.Module
	var err error
	if f.isComponent {
		mod, err = rt.LoadComponent(ctx, f.bytes)
	} else {
		mod, err = rt.LoadWASM(ctx, f.bytes, f.cfg.WIT)
	}
	if err != nil {
		return nil, runtimewasm.NewLoadWASMError(err)
	}
	if err := mod.Compile(ctx); err != nil {
		return nil, runtimewasm.NewCompileModuleError(err)
	}
	return mod, nil
}

// Create returns a process.FactoryFunc for process spawning.
// Every PID gets a fresh backend runtime sharing this factory's compilation
// cache, a fresh HostRegistry.Fork(), frozen verified component bytes, and
// owns module and resource table lifetimes.
func (f *ActorFactory) Create() process.FactoryFunc {
	return func() (process.Process, error) {
		if !f.tryRetain() {
			return nil, ErrActorFactoryClosed
		}

		spawnCtx := context.Background()
		if err := f.ensurePin(spawnCtx); err != nil {
			f.gen.release()
			if f.closed.Load() {
				return nil, ErrActorFactoryClosed
			}
			return nil, err
		}
		if f.closed.Load() {
			f.gen.release()
			return nil, ErrActorFactoryClosed
		}

		rt, err := wasmrt.NewWithConfig(spawnCtx, f.runtimeConfig())
		if err != nil {
			f.gen.release()
			return nil, err
		}

		forkedHosts := f.hostRegistry.Fork()
		var hostBuffers *preview2.HostBufferBudget
		if f.hostBufferBytes > 0 {
			hostBuffers = preview2.NewHostBufferBudget(uint64(f.hostBufferBytes))
		}
		resourceTable := preview2.NewResourceTableWithBudgets(4096, preview2.NewSocketBudget(f.cfg.Limits().EffectiveMaxOpenSockets()), hostBuffers)
		forkedHosts.SetSharedResources(resourceTable)

		cleanupRuntime := func() {
			forkedHosts.CloseResources()
			_ = rt.Close(context.Background())
		}
		fail := func(err error) (process.Process, error) {
			cleanupRuntime()
			f.gen.release()
			return nil, err
		}

		if err := forkedHosts.EnsureImports(spawnCtx, rt, f.cfg.Imports, f.isComponent); err != nil {
			return fail(err)
		}

		mod, err := f.loadAndCompile(spawnCtx, rt)
		if err != nil {
			return fail(err)
		}

		proc := wasmengine.NewProcess(
			mod,
			f.cfg.EffectiveTransport(),
			f.cfg.WASI,
			f.cfg.EffectiveLimitsConfig(),
			f.fsRegistry,
		)

		mb := f.cfg.Mailbox()
		actorLimits := actor.Limits{
			Capacity:     mb.EffectiveCapacity(),
			Bytes:        mb.EffectiveBytes(),
			MessageBytes: mb.EffectiveMessageBytes(),
		}

		released := atomic.Bool{}
		releaseGen := func() {
			if released.CompareAndSwap(false, true) {
				f.gen.release()
			}
		}

		actorProc := wasmengine.NewActorProcess(proc, actorLimits, func() {
			cleanupRuntime()
			releaseGen()
		})
		actorProc.SetMemoryBudget(memorybudget.New(uint64(f.memoryPages) * uint64(api.MinProcessMemoryBytesMultiple)))
		actorProc.SetSocketBudget(resourceTable.SocketBudget())
		actorProc.SetHostBufferBudget(resourceTable.HostBufferBudget())

		f.mu.Lock()
		published := !f.closed.Load()
		f.mu.Unlock()
		if !published {
			actorProc.Close()
			return nil, ErrActorFactoryClosed
		}

		return actorProc, nil
	}
}
