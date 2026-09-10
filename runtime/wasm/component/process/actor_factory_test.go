// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	fsapi "github.com/wippyai/runtime/api/fs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	processapi "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	api "github.com/wippyai/runtime/api/runtime/wasm"
	"github.com/wippyai/runtime/api/security"
	wasmcomponent "github.com/wippyai/runtime/runtime/wasm/component"
	wasmengine "github.com/wippyai/runtime/runtime/wasm/engine"
	"github.com/wippyai/runtime/runtime/wasm/host/wippy/hosts/actor"
	systempayload "github.com/wippyai/runtime/system/payload"
	jsonpayload "github.com/wippyai/runtime/system/payload/json"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"go.uber.org/zap"
)

func testActorHostProfile() wasmcomponent.HostProfile {
	return wasmcomponent.HostProfile{
		Name:          "wippy:actor",
		Aliases:       []string{actor.Namespace},
		ComponentOnly: true,
		Register: func(_ context.Context, rt *wasmrt.Runtime) error {
			return rt.RegisterHost(actor.NewHost())
		},
	}
}

type testMemoryFS struct {
	files map[string][]byte
	mu    sync.RWMutex
}

func newTestMemoryFS() *testMemoryFS {
	return &testMemoryFS{files: make(map[string][]byte)}
}

func (m *testMemoryFS) set(path string, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[path] = data
}

func (m *testMemoryFS) GetFS(_ string) (fsapi.FS, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	mapFS := fstest.MapFS{}
	for k, v := range m.files {
		mapFS[k] = &fstest.MapFile{Data: v, Mode: fs.FileMode(0o644)}
	}
	return fsapi.NewReadOnlyFS(mapFS), true
}

func testDecodeContext() context.Context {
	ctx := ctxapi.NewRootContext()
	transcoder := systempayload.NewTranscoder()
	jsonpayload.Register(transcoder)
	return payload.WithTranscoder(ctx, transcoder)
}

func loadActorWASM(t *testing.T) ([]byte, string) {
	t.Helper()
	data, err := os.ReadFile("../../engine/testdata/actor.wasm")
	require.NoError(t, err)
	sum := sha256.Sum256(data)
	hash := "sha256:" + hex.EncodeToString(sum[:])
	return data, hash
}

func TestActorFactory_IsolatedSpawnResources(t *testing.T) {
	actorBytes, _ := loadActorWASM(t)

	hostReg := wasmcomponent.NewHostRegistry()
	require.NoError(t, hostReg.RegisterProfiles(testActorHostProfile()))

	cfg := &api.ProcessConfig{
		Method:  "run",
		Imports: []registry.ID{registry.ParseID("wippy:actor")},
	}
	cfg.SetOptions(api.ProcessOptions{
		Limits: api.ProcessLimitsConfig{
			MemoryBytes: 64 * 1024 * 1024,
		},
		Mailbox: api.ProcessMailboxConfig{
			Capacity:     64,
			Bytes:        4 * 1024 * 1024,
			MessageBytes: 512 * 1024,
		},
	})

	factory := NewActorFactory(actorBytes, true, cfg, hostReg, nil)
	spawnFunc := factory.Create()

	proc1, err := spawnFunc()
	require.NoError(t, err)
	require.NotNil(t, proc1)

	proc2, err := spawnFunc()
	require.NoError(t, err)
	require.NotNil(t, proc2)

	actorProc1, ok := proc1.(*wasmengine.ActorProcess)
	require.True(t, ok)
	actorProc2, ok := proc2.(*wasmengine.ActorProcess)
	require.True(t, ok)

	// Verify both can initialize independently with distinct PIDs
	ctx1, frame1 := ctxapi.OpenFrameContext(ctxapi.NewRootContext())
	defer frame1.Close()
	pid1 := pid.PID{Node: "local", Host: "actors", UniqID: "p1"}
	require.NoError(t, runtimeapi.SetFramePID(ctx1, pid1))

	ctx2, frame2 := ctxapi.OpenFrameContext(ctxapi.NewRootContext())
	defer frame2.Close()
	pid2 := pid.PID{Node: "local", Host: "actors", UniqID: "p2"}
	require.NoError(t, runtimeapi.SetFramePID(ctx2, pid2))

	require.NoError(t, actorProc1.Init(ctx1, "run", nil))
	require.NoError(t, actorProc2.Init(ctx2, "run", nil))

	// Close proc1 and verify proc2 is unaffected
	actorProc1.Close()

	var out processapi.StepOutput
	require.NoError(t, actorProc2.Step(nil, &out))
	assert.True(t, out.IsIdle())

	actorProc2.Close()
}

func TestActorFactory_FailureCleanup(t *testing.T) {
	actorBytes, _ := loadActorWASM(t)

	// Host registry without wippy:actor profile registered
	hostReg := wasmcomponent.NewHostRegistry()

	cfg := &api.ProcessConfig{
		Method:  "run",
		Imports: []registry.ID{registry.ParseID("wippy:actor")},
	}
	factory := NewActorFactory(actorBytes, true, cfg, hostReg, nil)
	spawnFunc := factory.Create()

	proc, err := spawnFunc()
	require.Error(t, err)
	assert.Nil(t, proc)
	assert.Contains(t, err.Error(), "unsupported wasm host import")
}

func TestActorFactory_ClosedRejectsSpawn(t *testing.T) {
	actorBytes, _ := loadActorWASM(t)
	hostReg := wasmcomponent.NewHostRegistry()
	require.NoError(t, hostReg.RegisterProfiles(testActorHostProfile()))

	cfg := &api.ProcessConfig{
		Method:  "run",
		Imports: []registry.ID{registry.ParseID("wippy:actor")},
	}
	factory := NewActorFactory(actorBytes, true, cfg, hostReg, nil)
	spawnFunc := factory.Create()

	factory.Close()
	assert.True(t, factory.IsClosed())

	proc, err := spawnFunc()
	require.ErrorIs(t, err, ErrActorFactoryClosed)
	assert.Nil(t, proc)
}

func TestManager_AddAndSpawnActor(t *testing.T) {
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

	entryJSON := fmt.Sprintf(`{"fs":"test:fs","path":"actor.wasm","hash":%q,"method":"run","imports":["wippy:actor"]}`, hash)
	entry := registry.Entry{
		ID:   registry.ParseID("app.test:actor"),
		Kind: api.ProcessWASM,
		Data: payload.NewPayload(entryJSON, payload.JSON),
	}

	require.NoError(t, m.Add(ctx, entry))
	require.Len(t, bus.events, 1)
	assert.Equal(t, processapi.FactoryRegister, bus.events[0].Kind)

	regEntry, ok := bus.events[0].Data.(*processapi.FactoryEntry)
	require.True(t, ok)
	require.NotNil(t, regEntry.Factory)

	// Spawning does not reload from filesystem; delete file to prove frozen bytes
	fsReg.set("actor.wasm", []byte("corrupted"))

	proc, err := regEntry.Factory()
	require.NoError(t, err)
	require.NotNil(t, proc)

	ctxFrame, frame := ctxapi.OpenFrameContext(ctxapi.NewRootContext())
	defer frame.Close()
	pID := pid.PID{Node: "local", Host: "actors", UniqID: "spawn1"}
	require.NoError(t, runtimeapi.SetFramePID(ctxFrame, pID))

	require.NoError(t, proc.Init(ctxFrame, "run", nil))
	var out processapi.StepOutput
	require.NoError(t, proc.Step(nil, &out))
	assert.True(t, out.IsIdle())

	proc.Close()
}

func TestManager_FailedUpdatePreservesConfig(t *testing.T) {
	actorBytes, hash := loadActorWASM(t)

	fsReg := newTestMemoryFS()
	fsReg.set("actor.wasm", actorBytes)
	fsReg.set("bad.wasm", []byte("not-wasm-bytes"))

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
	savedCfg := m.getConfig(entryID)
	require.NotNil(t, savedCfg)
	oldFactory := savedCfg.factory

	// Attempt update with invalid WASM bytes
	badSum := sha256.Sum256([]byte("not-wasm-bytes"))
	badHash := "sha256:" + hex.EncodeToString(badSum[:])
	badJSON := fmt.Sprintf(`{"fs":"test:fs","path":"bad.wasm","hash":%q,"method":"run","imports":["wippy:actor"]}`, badHash)
	badEntry := registry.Entry{
		ID:   entryID,
		Kind: api.ProcessWASM,
		Data: payload.NewPayload(badJSON, payload.JSON),
	}

	err := m.Update(ctx, badEntry)
	require.Error(t, err)

	// Config and factory must be preserved
	currentCfg := m.getConfig(entryID)
	require.NotNil(t, currentCfg)
	assert.Same(t, oldFactory, currentCfg.factory)
	assert.False(t, oldFactory.IsClosed())

	// Factory can still spawn working processes
	proc, err := oldFactory.Create()()
	require.NoError(t, err)
	require.NotNil(t, proc)
	proc.Close()
}

func TestManager_StopBehavior(t *testing.T) {
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
	cfg := m.getConfig(entryID)
	require.NotNil(t, cfg)
	factory := cfg.factory

	// Stop manager
	m.Stop()
	assert.False(t, m.isStarted())
	assert.True(t, factory.IsClosed())

	// Subsequent spawn rejected
	proc, err := factory.Create()()
	require.ErrorIs(t, err, ErrActorFactoryClosed)
	assert.Nil(t, proc)

	// Stop is idempotent
	assert.NotPanics(t, func() {
		m.Stop()
	})
}

func TestManager_InvalidateRefreezesBytes(t *testing.T) {
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

	// Invalidate refreezes bytes and updates factory
	m.Invalidate(ctx, []registry.ID{entryID})

	newCfg := m.getConfig(entryID)
	require.NotNil(t, newCfg)
	assert.NotSame(t, oldFactory, newCfg.factory)
	assert.True(t, oldFactory.IsClosed())
	assert.False(t, newCfg.factory.IsClosed())

	proc, err := newCfg.factory.Create()()
	require.NoError(t, err)
	require.NotNil(t, proc)
	proc.Close()
}

func TestManager_DeleteCleansUpFactory(t *testing.T) {
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
	cfg := m.getConfig(entryID)
	require.NotNil(t, cfg)
	factory := cfg.factory

	require.NoError(t, m.Delete(ctx, entry))
	assert.Nil(t, m.getConfig(entryID))
	assert.True(t, factory.IsClosed())

	proc, err := factory.Create()()
	require.ErrorIs(t, err, ErrActorFactoryClosed)
	assert.Nil(t, proc)
}

func TestManager_FailedAddCleansUpTempRuntime(t *testing.T) {
	fsReg := newTestMemoryFS()
	fsReg.set("invalid.wasm", []byte("invalid-wasm-bytes"))

	bus := &testBus{}
	m := NewManager(zap.NewNop(), bus, fsReg)
	require.NoError(t, m.RegisterHostProfiles(testActorHostProfile()))

	awaitSvc := &testPrepareAwaitService{result: event.AwaitResult{Accepted: true}}
	ctx := event.WithAwaitService(testDecodeContext(), awaitSvc)

	require.NoError(t, m.Start(ctx))
	defer m.Stop()

	badSum := sha256.Sum256([]byte("invalid-wasm-bytes"))
	badHash := "sha256:" + hex.EncodeToString(badSum[:])
	entryID := registry.ParseID("app.test:invalid")
	entryJSON := fmt.Sprintf(`{"fs":"test:fs","path":"invalid.wasm","hash":%q,"method":"run","imports":["wippy:actor"]}`, badHash)
	entry := registry.Entry{
		ID:   entryID,
		Kind: api.ProcessWASM,
		Data: payload.NewPayload(entryJSON, payload.JSON),
	}

	err := m.Add(ctx, entry)
	require.Error(t, err)
	assert.Nil(t, m.getConfig(entryID))
	assert.Empty(t, bus.events)
}

func TestActorFactory_NoCapturedRegistrationDeadlines(t *testing.T) {
	actorBytes, hash := loadActorWASM(t)

	fsReg := newTestMemoryFS()
	fsReg.set("actor.wasm", actorBytes)

	bus := &testBus{}
	m := NewManager(zap.NewNop(), bus, fsReg)
	require.NoError(t, m.RegisterHostProfiles(testActorHostProfile()))

	awaitSvc := &testPrepareAwaitService{result: event.AwaitResult{Accepted: true}}
	regCtx, cancelReg := context.WithCancel(event.WithAwaitService(testDecodeContext(), awaitSvc))

	require.NoError(t, m.Start(context.Background()))
	defer m.Stop()

	entryID := registry.ParseID("app.test:actor")
	entryJSON := fmt.Sprintf(`{"fs":"test:fs","path":"actor.wasm","hash":%q,"method":"run","imports":["wippy:actor"]}`, hash)
	entry := registry.Entry{
		ID:   entryID,
		Kind: api.ProcessWASM,
		Data: payload.NewPayload(entryJSON, payload.JSON),
	}

	require.NoError(t, m.Add(regCtx, entry))
	cfg := m.getConfig(entryID)
	require.NotNil(t, cfg)

	// Now cancel the registration context entirely
	cancelReg()

	// Spawning now should NOT fail with context cancelled
	proc, err := cfg.factory.Create()()
	require.NoError(t, err)
	require.NotNil(t, proc)
	proc.Close()
}

func TestCoreProcessSupport(t *testing.T) {
	coreBytes, err := os.ReadFile("../../../../tests/app/src/test/wasm/answer_raw.wasm")
	require.NoError(t, err)

	hostReg := wasmcomponent.NewHostRegistry()
	cfg := &api.ProcessConfig{
		Method: "answer",
	}

	// Core process without actor import is supported safely
	factory := NewActorFactory(coreBytes, false, cfg, hostReg, nil)
	proc, err := factory.Create()()
	require.NoError(t, err)
	require.NotNil(t, proc)

	proc.Close()
}

func TestActorFactory_LateSpawnCloseRaceWithBarrier(t *testing.T) {
	actorBytes, _ := loadActorWASM(t)

	barrierEntered := make(chan struct{})
	barrierRelease := make(chan struct{})

	barrierProfile := wasmcomponent.HostProfile{
		Name:          "wippy:actor",
		Aliases:       []string{actor.Namespace},
		ComponentOnly: true,
		Register: func(_ context.Context, rt *wasmrt.Runtime) error {
			close(barrierEntered)
			<-barrierRelease
			return rt.RegisterHost(actor.NewHost())
		},
	}

	hostReg := wasmcomponent.NewHostRegistry()
	require.NoError(t, hostReg.RegisterProfiles(barrierProfile))

	cfg := &api.ProcessConfig{
		Method:  "run",
		Imports: []registry.ID{registry.ParseID("wippy:actor")},
	}
	factory := NewActorFactory(actorBytes, true, cfg, hostReg, nil)
	spawnFunc := factory.Create()

	type spawnResult struct {
		proc processapi.Process
		err  error
	}
	resChan := make(chan spawnResult, 1)

	go func() {
		proc, err := spawnFunc()
		resChan <- spawnResult{proc: proc, err: err}
	}()

	// Wait until spawn enters host registration barrier
	<-barrierEntered

	// Invalidate the factory while spawn is in-flight
	factory.Close()
	assert.True(t, factory.IsClosed())

	// Release the barrier so compile/load finishes and reaches publication recheck
	close(barrierRelease)

	res := <-resChan
	require.ErrorIs(t, res.err, ErrActorFactoryClosed)
	assert.Nil(t, res.proc)
}

func TestManager_AddPreservesSecurityMetadata(t *testing.T) {
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

	entryJSON := fmt.Sprintf(`{"fs":"test:fs","path":"actor.wasm","hash":%q,"method":"run","imports":["wippy:actor"],"security":{"actor":{"id":"app.test:actor"}}}`, hash)
	expectedSec := &security.Config{
		Actor: security.Actor{ID: "app.test:actor"},
	}
	entry := registry.Entry{
		ID:   registry.ParseID("app.test:actor"),
		Kind: api.ProcessWASM,
		Data: payload.NewPayload(entryJSON, payload.JSON),
	}

	require.NoError(t, m.Add(ctx, entry))
	require.Len(t, bus.events, 1)
	regEntry, ok := bus.events[0].Data.(*processapi.FactoryEntry)
	require.True(t, ok)
	assert.Equal(t, expectedSec, regEntry.Meta.Security)

	cfg := m.getConfig(entry.ID)
	require.NotNil(t, cfg)
	assert.Equal(t, expectedSec, cfg.security)
}

func TestActorFactory_SharedSocketBudget(t *testing.T) {
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
	spawnFunc := factory.Create()

	proc1, err := spawnFunc()
	require.NoError(t, err)
	actorProc1, ok := proc1.(*wasmengine.ActorProcess)
	require.True(t, ok)
	require.NotNil(t, actorProc1.SocketBudget())
	assert.Equal(t, 4, actorProc1.SocketBudget().Capacity())

	proc2, err := spawnFunc()
	require.NoError(t, err)
	actorProc2, ok := proc2.(*wasmengine.ActorProcess)
	require.True(t, ok)
	require.NotNil(t, actorProc2.SocketBudget())
	assert.Equal(t, 4, actorProc2.SocketBudget().Capacity())

	// Socket budgets must be distinct instances
	assert.True(t, actorProc1.SocketBudget() != actorProc2.SocketBudget())

	// Acquire in proc1 does not affect proc2
	lease, err := actorProc1.SocketBudget().Acquire()
	require.NoError(t, err)
	assert.Equal(t, 1, actorProc1.SocketBudget().Used())
	assert.Equal(t, 0, actorProc2.SocketBudget().Used())

	lease.Release()
	assert.Equal(t, 0, actorProc1.SocketBudget().Used())

	actorProc1.Close()
	actorProc2.Close()
}
