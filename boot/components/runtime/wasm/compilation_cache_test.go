// SPDX-License-Identifier: MPL-2.0

package wasm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/event"
	fsapi "github.com/wippyai/runtime/api/fs"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/internal/toolchain"
	wasmcomponent "github.com/wippyai/runtime/runtime/wasm/component"
	wasmproc "github.com/wippyai/runtime/runtime/wasm/component/process"
	systempayload "github.com/wippyai/runtime/system/payload"
	jsonpayload "github.com/wippyai/runtime/system/payload/json"
	"github.com/wippyai/wasm-runtime/asyncify"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

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

type testPrepareAwaitService struct {
	result event.AwaitResult
}

type testPrepareAwaitWaiter struct {
	result event.AwaitResult
}

func (w *testPrepareAwaitWaiter) Wait() event.AwaitResult { return w.result }
func (w *testPrepareAwaitWaiter) Close()                  {}

func (a *testPrepareAwaitService) Prepare(context.Context, event.System, event.Kind, event.Path, time.Duration) (event.AwaitWaiter, error) {
	return &testPrepareAwaitWaiter{result: a.result}, nil
}

func (a *testPrepareAwaitService) Await(context.Context, event.System, event.Kind, event.Path, time.Duration) event.AwaitResult {
	return a.result
}

func (a *testPrepareAwaitService) Start(context.Context) error { return nil }
func (a *testPrepareAwaitService) Stop() error                 { return nil }

func testDecodeContext() context.Context {
	ctx := ctxapi.NewRootContext()
	transcoder := systempayload.NewTranscoder()
	jsonpayload.Register(transcoder)
	return payload.WithTranscoder(ctx, transcoder)
}

func TestWASMCompilationCache_DiskPersistenceAndReuse(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "wasm-cache")
	wasmData, err := os.ReadFile("testdata/sockets_probe.wasm")
	require.NoError(t, err)
	sum := sha256.Sum256(wasmData)
	hash := "sha256:" + hex.EncodeToString(sum[:])

	fsReg := newTestMemoryFS()
	fsReg.set("probe.wasm", wasmData)

	awaitSvc := &testPrepareAwaitService{result: event.AwaitResult{Accepted: true}}
	ctx := event.WithAwaitService(testDecodeContext(), awaitSvc)
	bus := &testBus{}

	caches := wasmcomponent.Caches{
		Compilation: wasmcomponent.DirCompilationCache(cacheDir),
		Transform:   asyncify.NewMemoryTransformCache(),
	}

	// Phase 1: create manager with DirCompilationCache factory over cacheDir.
	mgr1 := wasmproc.NewManager(zap.NewNop(), bus, fsReg, caches)
	require.NoError(t, mgr1.Start(ctx))

	entryID1 := registry.ParseID("test:actor1")
	entryJSON1 := fmt.Sprintf(`{"fs":"test:fs","path":"probe.wasm","hash":%q,"method":"run"}`, hash)
	entry1 := registry.Entry{
		ID:   entryID1,
		Kind: wasmapi.ProcessWASM,
		Data: payload.NewPayload(entryJSON1, payload.JSON),
	}
	require.NoError(t, mgr1.Add(ctx, entry1))

	filesAfterFirstAdd := listFileNames(t, cacheDir)
	require.NotEmpty(t, filesAfterFirstAdd)

	// Phase 2: add same module under different entry ID.
	entryID2 := registry.ParseID("test:actor2")
	entryJSON2 := fmt.Sprintf(`{"fs":"test:fs","path":"probe.wasm","hash":%q,"method":"run"}`, hash)
	entry2 := registry.Entry{
		ID:   entryID2,
		Kind: wasmapi.ProcessWASM,
		Data: payload.NewPayload(entryJSON2, payload.JSON),
	}
	require.NoError(t, mgr1.Add(ctx, entry2))

	filesAfterSecondAdd := listFileNames(t, cacheDir)
	assert.Equal(t, filesAfterFirstAdd, filesAfterSecondAdd)

	mgr1.Stop()

	// Phase 3: brand-new manager over the same directory (simulating restart).
	mgr2 := wasmproc.NewManager(zap.NewNop(), bus, fsReg, caches)
	require.NoError(t, mgr2.Start(ctx))
	defer mgr2.Stop()

	entryID3 := registry.ParseID("test:actor3")
	entryJSON3 := fmt.Sprintf(`{"fs":"test:fs","path":"probe.wasm","hash":%q,"method":"run"}`, hash)
	entry3 := registry.Entry{
		ID:   entryID3,
		Kind: wasmapi.ProcessWASM,
		Data: payload.NewPayload(entryJSON3, payload.JSON),
	}
	require.NoError(t, mgr2.Add(ctx, entry3))

	filesAfterRestart := listFileNames(t, cacheDir)
	assert.Equal(t, filesAfterFirstAdd, filesAfterRestart)
}

func TestWASMCompilationCache_UnwritableFallback(t *testing.T) {
	parentFile := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(parentFile, []byte("file"), 0o644))
	badCacheDir := filepath.Join(parentFile, "wasm")

	core, observedLogs := observer.New(zap.WarnLevel)
	observedLogger := zap.New(core)

	ctx := ctxapi.NewRootContext()
	ctx = logapi.WithLogger(ctx, observedLogger)
	ctx = event.WithBus(ctx, testBus{})
	ctx = bootpkg.WithHandlerRegistry(ctx, bootpkg.NewHandlerRegistry())
	require.NoError(t, dispatcher.WithRegistry(ctx, testDispatcherRegistry{}))

	bootCfg := boot.NewConfig(boot.WithSection("wasm", map[string]any{
		"cache.enabled": true,
		"cache.dir":     badCacheDir,
	}))
	ctx = boot.WithConfig(ctx, bootCfg)

	component := EngineWithHostProfiles()
	loadedCtx, err := component.Load(ctx)
	require.NoError(t, err)

	starter, ok := component.(interface{ Start(context.Context) error })
	require.True(t, ok)
	require.NoError(t, starter.Start(loadedCtx))

	warnings := observedLogs.FilterLevelExact(zap.WarnLevel).All()
	require.Len(t, warnings, 1)
	assert.Equal(t, badCacheDir, warnings[0].ContextMap()["dir"])
	assert.NotEmpty(t, warnings[0].ContextMap()["error"])

	stopper, ok := component.(interface{ Stop(context.Context) error })
	require.True(t, ok)
	require.NoError(t, stopper.Stop(loadedCtx))
}

func TestWASMCompilationCache_DisabledCreatesNoDirectory(t *testing.T) {
	targetDir := filepath.Join(t.TempDir(), "should-not-exist")

	core, observedLogs := observer.New(zap.WarnLevel)
	observedLogger := zap.New(core)

	ctx := ctxapi.NewRootContext()
	ctx = logapi.WithLogger(ctx, observedLogger)
	ctx = event.WithBus(ctx, testBus{})
	ctx = bootpkg.WithHandlerRegistry(ctx, bootpkg.NewHandlerRegistry())
	require.NoError(t, dispatcher.WithRegistry(ctx, testDispatcherRegistry{}))

	bootCfg := boot.NewConfig(boot.WithSection("wasm", map[string]any{
		"cache.enabled": false,
		"cache.dir":     targetDir,
	}))
	ctx = boot.WithConfig(ctx, bootCfg)

	component := EngineWithHostProfiles()
	loadedCtx, err := component.Load(ctx)
	require.NoError(t, err)

	starter, ok := component.(interface{ Start(context.Context) error })
	require.True(t, ok)
	require.NoError(t, starter.Start(loadedCtx))

	_, err = os.Stat(targetDir)
	assert.True(t, os.IsNotExist(err))

	warnings := observedLogs.FilterLevelExact(zap.WarnLevel).All()
	assert.Empty(t, warnings)

	stopper, ok := component.(interface{ Stop(context.Context) error })
	require.True(t, ok)
	require.NoError(t, stopper.Stop(loadedCtx))
}

func TestWASMCompilationCache_DefaultDirUnderWippyCacheDir(t *testing.T) {
	tempCache := t.TempDir()
	t.Setenv("WIPPY_CACHE_DIR", tempCache)

	ctx := ctxapi.NewRootContext()
	ctx = logapi.WithLogger(ctx, zap.NewNop())
	ctx = event.WithBus(ctx, testBus{})
	ctx = bootpkg.WithHandlerRegistry(ctx, bootpkg.NewHandlerRegistry())
	require.NoError(t, dispatcher.WithRegistry(ctx, testDispatcherRegistry{}))

	component := EngineWithHostProfiles()
	loadedCtx, err := component.Load(ctx)
	require.NoError(t, err)

	starter, ok := component.(interface{ Start(context.Context) error })
	require.True(t, ok)
	require.NoError(t, starter.Start(loadedCtx))

	expectedDir := filepath.Join(tempCache, "wasm")
	info, err := os.Stat(expectedDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	stopper, ok := component.(interface{ Stop(context.Context) error })
	require.True(t, ok)
	require.NoError(t, stopper.Stop(loadedCtx))
}

func TestWASMTransformCache_DiskPersistenceAndReuse(t *testing.T) {
	tempDir := t.TempDir()
	compilationDir := filepath.Join(tempDir, "compilation")
	transformDir := filepath.Join(tempDir, "transform")

	transCache, err := asyncify.NewDirTransformCache(transformDir)
	require.NoError(t, err)

	caches := wasmcomponent.Caches{
		Compilation: wasmcomponent.DirCompilationCache(compilationDir),
		Transform:   transCache,
	}

	wasmData, err := os.ReadFile("testdata/sockets_probe.wasm")
	require.NoError(t, err)
	sum := sha256.Sum256(wasmData)
	hash := "sha256:" + hex.EncodeToString(sum[:])

	fsReg := newTestMemoryFS()
	fsReg.set("probe.wasm", wasmData)

	awaitSvc := &testPrepareAwaitService{result: event.AwaitResult{Accepted: true}}
	ctx := event.WithAwaitService(testDecodeContext(), awaitSvc)
	bus := &testBus{}

	// Phase 1: create manager with directory-backed caches and host profiles.
	mgr1 := wasmproc.NewManager(zap.NewNop(), bus, fsReg, caches)
	require.NoError(t, mgr1.RegisterHostProfiles(DefaultHostProfiles(zap.NewNop(), nil)...))
	require.NoError(t, mgr1.Start(ctx))

	entryID1 := registry.ParseID("test:actor1")
	entryJSON1 := fmt.Sprintf(`{"fs":"test:fs","path":"probe.wasm","hash":%q,"method":"run","imports":["wasi:sockets","wasi:poll","wasi:clocks"]}`, hash)
	entry1 := registry.Entry{
		ID:   entryID1,
		Kind: wasmapi.ProcessWASM,
		Data: payload.NewPayload(entryJSON1, payload.JSON),
	}
	require.NoError(t, mgr1.Add(ctx, entry1))

	filesAfterFirstAdd := listFileNames(t, transformDir)
	require.NotEmpty(t, filesAfterFirstAdd)

	// Phase 2: add same module under different entry ID.
	entryID2 := registry.ParseID("test:actor2")
	entryJSON2 := fmt.Sprintf(`{"fs":"test:fs","path":"probe.wasm","hash":%q,"method":"run","imports":["wasi:sockets","wasi:poll","wasi:clocks"]}`, hash)
	entry2 := registry.Entry{
		ID:   entryID2,
		Kind: wasmapi.ProcessWASM,
		Data: payload.NewPayload(entryJSON2, payload.JSON),
	}
	require.NoError(t, mgr1.Add(ctx, entry2))

	filesAfterSecondAdd := listFileNames(t, transformDir)
	assert.Equal(t, filesAfterFirstAdd, filesAfterSecondAdd)

	mgr1.Stop()

	// Phase 3: brand-new manager over the same directory.
	mgr2 := wasmproc.NewManager(zap.NewNop(), bus, fsReg, caches)
	require.NoError(t, mgr2.RegisterHostProfiles(DefaultHostProfiles(zap.NewNop(), nil)...))
	require.NoError(t, mgr2.Start(ctx))
	defer mgr2.Stop()

	entryID3 := registry.ParseID("test:actor3")
	entryJSON3 := fmt.Sprintf(`{"fs":"test:fs","path":"probe.wasm","hash":%q,"method":"run","imports":["wasi:sockets","wasi:poll","wasi:clocks"]}`, hash)
	entry3 := registry.Entry{
		ID:   entryID3,
		Kind: wasmapi.ProcessWASM,
		Data: payload.NewPayload(entryJSON3, payload.JSON),
	}
	require.NoError(t, mgr2.Add(ctx, entry3))

	filesAfterRestart := listFileNames(t, transformDir)
	assert.Equal(t, filesAfterFirstAdd, filesAfterRestart)
}

func TestWASMBootComponent_TransformCacheDirectoryLayout(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "wasm-cache")

	ctx := ctxapi.NewRootContext()
	ctx = logapi.WithLogger(ctx, zap.NewNop())
	ctx = event.WithBus(ctx, testBus{})
	ctx = bootpkg.WithHandlerRegistry(ctx, bootpkg.NewHandlerRegistry())
	require.NoError(t, dispatcher.WithRegistry(ctx, testDispatcherRegistry{}))

	bootCfg := boot.NewConfig(boot.WithSection("wasm", map[string]any{
		"cache.enabled": true,
		"cache.dir":     cacheDir,
	}))
	ctx = boot.WithConfig(ctx, bootCfg)

	component := EngineWithHostProfiles()
	loadedCtx, err := component.Load(ctx)
	require.NoError(t, err)

	starter, ok := component.(interface{ Start(context.Context) error })
	require.True(t, ok)
	require.NoError(t, starter.Start(loadedCtx))

	identity, err := toolchain.ModuleIdentity("github.com/wippyai/wasm-runtime")
	require.NoError(t, err)

	expectedTransformDir := filepath.Join(cacheDir, "asyncify", identity)
	info, err := os.Stat(expectedTransformDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	stopper, ok := component.(interface{ Stop(context.Context) error })
	require.True(t, ok)
	require.NoError(t, stopper.Stop(loadedCtx))
}

func TestWASMBootComponent_UnresolvableIdentityFallback(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "wasm-cache")

	core, observedLogs := observer.New(zap.WarnLevel)
	observedLogger := zap.New(core)

	ctx := ctxapi.NewRootContext()
	ctx = logapi.WithLogger(ctx, observedLogger)
	ctx = event.WithBus(ctx, testBus{})
	ctx = bootpkg.WithHandlerRegistry(ctx, bootpkg.NewHandlerRegistry())
	require.NoError(t, dispatcher.WithRegistry(ctx, testDispatcherRegistry{}))

	bootCfg := boot.NewConfig(boot.WithSection("wasm", map[string]any{
		"cache.enabled": true,
		"cache.dir":     cacheDir,
	}))
	ctx = boot.WithConfig(ctx, bootCfg)

	// Inject unresolvable identity resolver via the unexported seam.
	failingResolver := func(string) (string, error) {
		return "", errors.New("simulated toolchain identity failure")
	}
	component := newEngine(failingResolver)
	loadedCtx, err := component.Load(ctx)
	require.NoError(t, err)

	starter, ok := component.(interface{ Start(context.Context) error })
	require.True(t, ok)
	require.NoError(t, starter.Start(loadedCtx))

	// Exactly one warning for unresolvable identity.
	warnings := observedLogs.FilterLevelExact(zap.WarnLevel).All()
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0].Message, "identity")

	// In-memory transform cache used: no <dir>/asyncify directory created.
	asyncifyDir := filepath.Join(cacheDir, "asyncify")
	_, err = os.Stat(asyncifyDir)
	assert.True(t, os.IsNotExist(err))

	// Compilation cache is still directory-backed: <dir> exists.
	info, err := os.Stat(cacheDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	stopper, ok := component.(interface{ Stop(context.Context) error })
	require.True(t, ok)
	require.NoError(t, stopper.Stop(loadedCtx))
}

func listFileNames(t *testing.T, dir string) []string {
	t.Helper()
	var names []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			rel, relErr := filepath.Rel(dir, path)
			if relErr == nil {
				names = append(names, rel)
			}
		}
		return nil
	})
	require.NoError(t, err)
	sort.Strings(names)
	return names
}
