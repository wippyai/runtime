// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	processapi "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	api "github.com/wippyai/runtime/api/runtime/wasm"
	wasmcomponent "github.com/wippyai/runtime/runtime/wasm/component"
	wasmengine "github.com/wippyai/runtime/runtime/wasm/engine"
	"github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/actor"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"go.uber.org/zap"
)

// These tests observe our pin/cache ownership and guest execution. Wazero's
// private compiled-module counters are not an API; cache-hit performance is
// measured separately rather than inferred from this lifecycle test.
type recordingGenerationCache struct {
	closes atomic.Int32
}

func (c *recordingGenerationCache) Close(context.Context) error {
	c.closes.Add(1)
	return nil
}

func TestFactoryGeneration_ClosesCacheAfterLastConcurrentRelease(t *testing.T) {
	cache := &recordingGenerationCache{}
	generation := &factoryGeneration{cache: cache}
	generation.refs.Store(1)
	const actors = 32
	for range actors {
		generation.retain()
	}
	generation.release() // factory owner
	require.Zero(t, cache.closes.Load())
	var workers sync.WaitGroup
	for range actors {
		workers.Go(generation.release)
	}
	workers.Wait()
	require.Equal(t, int32(1), cache.closes.Load())
	require.Zero(t, generation.refs.Load())
	require.True(t, generation.cacheClosed.Load())
}

func newCacheTestActorFactory(t *testing.T) *ActorFactory {
	t.Helper()
	actorBytes, _ := loadActorWASM(t)
	hostReg := wasmcomponent.NewHostRegistry()
	require.NoError(t, hostReg.RegisterProfiles(testActorHostProfile()))
	cfg := &api.ProcessConfig{
		Method:  "run",
		Imports: []registry.ID{registry.ParseID("wippy:actor")},
	}
	cfg.SetOptions(api.ProcessOptions{
		Limits: api.ProcessLimitsConfig{
			MemoryBytes:    64 * 1024 * 1024,
			MaxOpenSockets: 4,
		},
		Mailbox: api.ProcessMailboxConfig{
			Capacity:     64,
			Bytes:        4 * 1024 * 1024,
			MessageBytes: 512 * 1024,
		},
	})
	factory := NewActorFactory(actorBytes, true, cfg, hostReg, nil)
	t.Cleanup(factory.Close)
	return factory
}

func initCacheTestActor(t *testing.T, proc processapi.Process, uniq string) *wasmengine.ActorProcess {
	t.Helper()
	actorProc, ok := proc.(*wasmengine.ActorProcess)
	require.True(t, ok)
	ctx, frame := ctxapi.OpenFrameContext(ctxapi.NewRootContext())
	t.Cleanup(func() { _ = frame.Close() })
	pID := pid.PID{Node: "local", Host: "actors", UniqID: uniq}
	require.NoError(t, runtimeapi.SetFramePID(ctx, pID))
	require.NoError(t, actorProc.Init(ctx, "run", nil))
	return actorProc
}

func requireIdleStep(t *testing.T, proc processapi.Process) {
	t.Helper()
	var out processapi.StepOutput
	require.NoError(t, proc.Step(nil, &out))
	assert.True(t, out.IsIdle())
}

func requireGenerationDrained(t *testing.T, factory *ActorFactory) {
	t.Helper()
	require.Equal(t, int32(0), factory.gen.refs.Load())
	require.True(t, factory.gen.cacheClosed.Load())
}

func TestActorFactory_PinKeepsCodeAfterLastActorClose(t *testing.T) {
	factory := newCacheTestActorFactory(t)
	require.NoError(t, factory.warm(context.Background()))
	require.Equal(t, int32(1), factory.gen.refs.Load())
	pinned := factory.pinRT
	require.NotNil(t, pinned)

	spawn := factory.Create()
	proc1, err := spawn()
	require.NoError(t, err)
	proc2, err := spawn()
	require.NoError(t, err)
	require.Equal(t, int32(3), factory.gen.refs.Load())

	actor1 := initCacheTestActor(t, proc1, "pin-keep-1")
	actor2 := initCacheTestActor(t, proc2, "pin-keep-2")
	requireIdleStep(t, actor1)
	requireIdleStep(t, actor2)
	assert.True(t, actor1.SocketBudget() != actor2.SocketBudget())

	actor1.Close()
	requireIdleStep(t, actor2)
	require.Equal(t, int32(2), factory.gen.refs.Load())

	actor2.Close()
	require.Equal(t, int32(1), factory.gen.refs.Load())
	require.Same(t, pinned, factory.pinRT)
	require.False(t, factory.gen.cacheClosed.Load())

	proc3, err := spawn()
	require.NoError(t, err)
	actor3 := initCacheTestActor(t, proc3, "pin-keep-3")
	requireIdleStep(t, actor3)
	actor3.Close()
	require.Equal(t, int32(1), factory.gen.refs.Load())
	require.Same(t, pinned, factory.pinRT)
	require.False(t, factory.gen.cacheClosed.Load())

	factory.Close()
	requireGenerationDrained(t, factory)
}

func TestActorFactory_LazyPinInitializer(t *testing.T) {
	factory := newCacheTestActorFactory(t)
	require.Nil(t, factory.pinRT)
	require.False(t, factory.gen.cacheClosed.Load())

	spawn := factory.Create()
	proc1, err := spawn()
	require.NoError(t, err)
	require.NotNil(t, factory.pinRT)
	pinned := factory.pinRT

	actor1 := initCacheTestActor(t, proc1, "lazy-1")
	requireIdleStep(t, actor1)
	actor1.Close()
	require.Same(t, pinned, factory.pinRT)

	proc2, err := spawn()
	require.NoError(t, err)
	actor2 := initCacheTestActor(t, proc2, "lazy-2")
	requireIdleStep(t, actor2)
	actor2.Close()

	factory.Close()
	requireGenerationDrained(t, factory)
}

func TestActorFactory_PinCloseLeavesLiveActorCodeRefs(t *testing.T) {
	factory := newCacheTestActorFactory(t)
	require.NoError(t, factory.warm(context.Background()))

	spawn := factory.Create()
	proc1, err := spawn()
	require.NoError(t, err)
	proc2, err := spawn()
	require.NoError(t, err)

	actor1 := initCacheTestActor(t, proc1, "pin-close-1")
	actor2 := initCacheTestActor(t, proc2, "pin-close-2")
	requireIdleStep(t, actor1)
	requireIdleStep(t, actor2)

	factory.Close()
	assert.True(t, factory.IsClosed())
	assert.Nil(t, factory.pinRT)
	require.False(t, factory.gen.cacheClosed.Load())
	require.Equal(t, int32(2), factory.gen.refs.Load())

	_, err = spawn()
	require.ErrorIs(t, err, ErrActorFactoryClosed)

	requireIdleStep(t, actor1)
	requireIdleStep(t, actor2)

	actor1.Close()
	requireIdleStep(t, actor2)
	require.False(t, factory.gen.cacheClosed.Load())

	actor2.Close()
	requireGenerationDrained(t, factory)
}

func TestActorFactory_DuplicateCloseDoesNotCloseSiblingCache(t *testing.T) {
	left := newCacheTestActorFactory(t)
	right := newCacheTestActorFactory(t)
	require.NoError(t, left.warm(context.Background()))
	require.NoError(t, right.warm(context.Background()))
	require.NotSame(t, left.gen.cache, right.gen.cache)

	rightSpawn := right.Create()
	rightProc, err := rightSpawn()
	require.NoError(t, err)
	rightActor := initCacheTestActor(t, rightProc, "sibling-right")
	requireIdleStep(t, rightActor)
	rightPin := right.pinRT
	require.NotNil(t, rightPin)

	left.Close()
	left.Close()
	left.Close()
	requireGenerationDrained(t, left)
	require.Equal(t, int32(2), right.gen.refs.Load())
	require.False(t, right.gen.cacheClosed.Load())
	require.Same(t, rightPin, right.pinRT)
	requireIdleStep(t, rightActor)

	rightActor.Close()
	right.Close()
	requireGenerationDrained(t, right)
}

func TestActorFactory_FailedHostRegistrationDoesNotLeakGenerationRef(t *testing.T) {
	actorBytes, _ := loadActorWASM(t)
	hostReg := wasmcomponent.NewHostRegistry()
	cfg := &api.ProcessConfig{
		Method:  "run",
		Imports: []registry.ID{registry.ParseID("wippy:actor")},
	}
	factory := NewActorFactory(actorBytes, true, cfg, hostReg, nil)
	t.Cleanup(factory.Close)

	proc, err := factory.Create()()
	require.Error(t, err)
	assert.Nil(t, proc)
	assert.Contains(t, err.Error(), "unsupported wasm host import")
	require.Equal(t, int32(1), factory.gen.refs.Load())
	require.False(t, factory.gen.cacheClosed.Load())

	factory.Close()
	requireGenerationDrained(t, factory)
}

func TestActorFactory_FailedSpawnHostRegistrationAfterWarmDoesNotLeak(t *testing.T) {
	actorBytes, _ := loadActorWASM(t)
	var registers atomic.Int32
	hostReg := wasmcomponent.NewHostRegistry()
	require.NoError(t, hostReg.RegisterProfiles(wasmcomponent.HostProfile{
		Name:          "wippy:actor",
		Aliases:       []string{actor.Namespace},
		ComponentOnly: true,
		Register: func(_ context.Context, rt *wasmrt.Runtime) error {
			if registers.Add(1) == 1 {
				return rt.RegisterHost(actor.NewHost())
			}
			return errors.New("host registration failed")
		},
	}))
	cfg := &api.ProcessConfig{
		Method:  "run",
		Imports: []registry.ID{registry.ParseID("wippy:actor")},
	}
	factory := NewActorFactory(actorBytes, true, cfg, hostReg, nil)
	t.Cleanup(factory.Close)

	require.NoError(t, factory.warm(context.Background()))
	pinned := factory.pinRT
	require.NotNil(t, pinned)

	proc, err := factory.Create()()
	require.Error(t, err)
	assert.Nil(t, proc)
	assert.Contains(t, err.Error(), "host registration failed")
	require.Equal(t, int32(1), factory.gen.refs.Load())
	require.Same(t, pinned, factory.pinRT)
	require.False(t, factory.gen.cacheClosed.Load())

	factory.Close()
	requireGenerationDrained(t, factory)
}

func TestActorFactory_FailedWarmDoesNotLeakGeneration(t *testing.T) {
	hostReg := wasmcomponent.NewHostRegistry()
	require.NoError(t, hostReg.RegisterProfiles(testActorHostProfile()))
	cfg := &api.ProcessConfig{
		Method:  "run",
		Imports: []registry.ID{registry.ParseID("wippy:actor")},
	}
	factory := NewActorFactory([]byte("not-a-wasm-module"), true, cfg, hostReg, nil)
	t.Cleanup(factory.Close)

	err := factory.warm(context.Background())
	require.Error(t, err)
	require.Equal(t, int32(1), factory.gen.refs.Load())
	require.Nil(t, factory.pinRT)

	factory.Close()
	requireGenerationDrained(t, factory)
}

func TestActorFactory_WarmCloseRaceKeepsCacheAliveUntilWarmUnwinds(t *testing.T) {
	actorBytes, _ := loadActorWASM(t)
	barrierEntered := make(chan struct{})
	barrierRelease := make(chan struct{})

	hostReg := wasmcomponent.NewHostRegistry()
	require.NoError(t, hostReg.RegisterProfiles(wasmcomponent.HostProfile{
		Name:          "wippy:actor",
		Aliases:       []string{actor.Namespace},
		ComponentOnly: true,
		Register: func(_ context.Context, rt *wasmrt.Runtime) error {
			close(barrierEntered)
			<-barrierRelease
			return rt.RegisterHost(actor.NewHost())
		},
	}))
	cfg := &api.ProcessConfig{
		Method:  "run",
		Imports: []registry.ID{registry.ParseID("wippy:actor")},
	}
	factory := NewActorFactory(actorBytes, true, cfg, hostReg, nil)
	t.Cleanup(factory.Close)

	errChan := make(chan error, 1)
	go func() {
		errChan <- factory.warm(context.Background())
	}()

	<-barrierEntered
	require.Equal(t, int32(2), factory.gen.refs.Load())
	require.False(t, factory.gen.cacheClosed.Load())

	factory.Close()
	require.True(t, factory.IsClosed())
	require.Equal(t, int32(1), factory.gen.refs.Load())
	require.False(t, factory.gen.cacheClosed.Load())

	close(barrierRelease)
	require.ErrorIs(t, <-errChan, ErrActorFactoryClosed)
	requireGenerationDrained(t, factory)
}

func TestActorFactory_LateSpawnCloseRaceAfterWarmDoesNotLeakRef(t *testing.T) {
	actorBytes, _ := loadActorWASM(t)
	barrierEntered := make(chan struct{})
	barrierRelease := make(chan struct{})
	var registers atomic.Int32

	hostReg := wasmcomponent.NewHostRegistry()
	require.NoError(t, hostReg.RegisterProfiles(wasmcomponent.HostProfile{
		Name:          "wippy:actor",
		Aliases:       []string{actor.Namespace},
		ComponentOnly: true,
		Register: func(_ context.Context, rt *wasmrt.Runtime) error {
			if registers.Add(1) == 1 {
				return rt.RegisterHost(actor.NewHost())
			}
			close(barrierEntered)
			<-barrierRelease
			return rt.RegisterHost(actor.NewHost())
		},
	}))
	cfg := &api.ProcessConfig{
		Method:  "run",
		Imports: []registry.ID{registry.ParseID("wippy:actor")},
	}
	factory := NewActorFactory(actorBytes, true, cfg, hostReg, nil)
	t.Cleanup(factory.Close)
	require.NoError(t, factory.warm(context.Background()))

	type spawnResult struct {
		proc processapi.Process
		err  error
	}
	resChan := make(chan spawnResult, 1)
	go func() {
		proc, err := factory.Create()()
		resChan <- spawnResult{proc: proc, err: err}
	}()

	<-barrierEntered
	factory.Close()
	close(barrierRelease)

	res := <-resChan
	require.ErrorIs(t, res.err, ErrActorFactoryClosed)
	assert.Nil(t, res.proc)
	requireGenerationDrained(t, factory)
}

func TestActorFactory_ConcurrentSpawnAndCloseNoRefLeak(t *testing.T) {
	factory := newCacheTestActorFactory(t)
	require.NoError(t, factory.warm(context.Background()))
	spawn := factory.Create()

	var mu sync.Mutex
	var live []processapi.Process
	var wg sync.WaitGroup
	const n = 8
	wg.Add(n + 1)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			proc, err := spawn()
			if err != nil {
				return
			}
			mu.Lock()
			live = append(live, proc)
			mu.Unlock()
		}()
	}
	go func() {
		defer wg.Done()
		factory.Close()
	}()
	wg.Wait()

	for _, proc := range live {
		if actorProc, ok := proc.(*wasmengine.ActorProcess); ok && actorProc.SocketBudget() != nil {
			initCacheTestActor(t, proc, fmt.Sprintf("race-%p", proc))
			requireIdleStep(t, proc)
		}
		proc.Close()
	}
	factory.Close()
	requireGenerationDrained(t, factory)
}

func TestActorFactory_CoreProcessPinLifecycle(t *testing.T) {
	coreBytes, err := os.ReadFile("../../../../tests/app/src/test/wasm/answer_raw.wasm")
	require.NoError(t, err)

	hostReg := wasmcomponent.NewHostRegistry()
	cfg := &api.ProcessConfig{Method: "answer"}
	factory := NewActorFactory(coreBytes, false, cfg, hostReg, nil)
	t.Cleanup(factory.Close)

	require.NoError(t, factory.warm(context.Background()))
	pinned := factory.pinRT
	require.NotNil(t, pinned)

	spawn := factory.Create()
	proc1, err := spawn()
	require.NoError(t, err)
	proc2, err := spawn()
	require.NoError(t, err)
	proc1.Close()
	proc2.Close()
	require.Same(t, pinned, factory.pinRT)
	require.False(t, factory.gen.cacheClosed.Load())

	proc3, err := spawn()
	require.NoError(t, err)
	proc3.Close()
	require.Equal(t, int32(1), factory.gen.refs.Load())

	factory.Close()
	requireGenerationDrained(t, factory)
}

func TestManager_InvalidateKeepsLiveActorAndNewFactory(t *testing.T) {
	actorBytes, hash := loadActorWASM(t)
	fsReg := newTestMemoryFS()
	fsReg.set("actor.wasm", actorBytes)

	bus := &testBus{}
	m := NewManager(zap.NewNop(), bus, fsReg)
	require.NoError(t, m.RegisterHostProfiles(testActorHostProfile()))

	awaitSvc := &testPrepareAwaitService{result: event.AwaitResult{Accepted: true}}
	ctx := event.WithAwaitService(testDecodeContext(), awaitSvc)
	require.NoError(t, m.Start(ctx))
	defer m.Stop()

	entryID := registry.ParseID("app.test:actor")
	entryJSON := fmt.Sprintf(`{"fs":"test:fs","path":"actor.wasm","hash":%q,"method":"run","imports":["wippy:actor"]}`, hash)
	entry := registry.Entry{
		ID:   entryID,
		Kind: api.ProcessWASM,
		Data: payload.NewPayload(entryJSON, payload.JSON),
	}
	require.NoError(t, m.Add(ctx, entry))

	oldCfg := m.getConfig(entryID)
	oldFactory := oldCfg.factory
	require.NotNil(t, oldFactory.pinRT)

	oldProc, err := oldFactory.Create()()
	require.NoError(t, err)
	oldActor := initCacheTestActor(t, oldProc, "invalidate-old")
	requireIdleStep(t, oldActor)

	m.Invalidate(ctx, []registry.ID{entryID})

	newCfg := m.getConfig(entryID)
	require.NotNil(t, newCfg)
	assert.NotSame(t, oldFactory, newCfg.factory)
	assert.True(t, oldFactory.IsClosed())
	assert.False(t, newCfg.factory.IsClosed())
	assert.Nil(t, oldFactory.pinRT)
	require.False(t, oldFactory.gen.cacheClosed.Load())
	require.Equal(t, int32(1), oldFactory.gen.refs.Load())

	_, err = oldFactory.Create()()
	require.ErrorIs(t, err, ErrActorFactoryClosed)
	requireIdleStep(t, oldActor)

	newProc, err := newCfg.factory.Create()()
	require.NoError(t, err)
	newActor := initCacheTestActor(t, newProc, "invalidate-new")
	requireIdleStep(t, newActor)

	oldActor.Close()
	requireGenerationDrained(t, oldFactory)
	requireIdleStep(t, newActor)
	newActor.Close()
}

func TestManager_StopKeepsLiveActorAndRejectsSpawn(t *testing.T) {
	actorBytes, hash := loadActorWASM(t)
	fsReg := newTestMemoryFS()
	fsReg.set("actor.wasm", actorBytes)

	bus := &testBus{}
	m := NewManager(zap.NewNop(), bus, fsReg)
	require.NoError(t, m.RegisterHostProfiles(testActorHostProfile()))

	awaitSvc := &testPrepareAwaitService{result: event.AwaitResult{Accepted: true}}
	ctx := event.WithAwaitService(testDecodeContext(), awaitSvc)
	require.NoError(t, m.Start(ctx))

	entryID := registry.ParseID("app.test:actor")
	entryJSON := fmt.Sprintf(`{"fs":"test:fs","path":"actor.wasm","hash":%q,"method":"run","imports":["wippy:actor"]}`, hash)
	entry := registry.Entry{
		ID:   entryID,
		Kind: api.ProcessWASM,
		Data: payload.NewPayload(entryJSON, payload.JSON),
	}
	require.NoError(t, m.Add(ctx, entry))

	factory := m.getConfig(entryID).factory
	proc, err := factory.Create()()
	require.NoError(t, err)
	actorProc := initCacheTestActor(t, proc, "stop-live")
	requireIdleStep(t, actorProc)

	m.Stop()
	assert.True(t, factory.IsClosed())
	assert.Nil(t, factory.pinRT)
	require.False(t, factory.gen.cacheClosed.Load())

	_, err = factory.Create()()
	require.ErrorIs(t, err, ErrActorFactoryClosed)
	requireIdleStep(t, actorProc)

	actorProc.Close()
	requireGenerationDrained(t, factory)
	assert.NotPanics(t, func() { m.Stop() })
}
