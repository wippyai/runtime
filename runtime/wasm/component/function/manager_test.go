package function

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	functionapi "github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/registry"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	runtimewasm "github.com/wippyai/runtime/runtime/wasm"
	wasmcomponent "github.com/wippyai/runtime/runtime/wasm/component"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
	"go.uber.org/zap"
)

type noopFunctionRegistry struct{}

func (noopFunctionRegistry) Call(context.Context, runtimeapi.Task) (*runtimeapi.Result, error) {
	return nil, nil
}

type noopDispatcher struct{}

func (noopDispatcher) Dispatch(_ dispatcher.Command) dispatcher.Handler {
	return dispatcher.HandlerFunc(func(_ context.Context, _ dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
		receiver.CompleteYield(tag, nil, nil)
		return nil
	})
}

func registerDefaultHostProfiles(t *testing.T, m *Manager) {
	t.Helper()
	require.NoError(t, m.RegisterHostProfiles(
		testFuncsHostProfile(),
		testWASI1HostProfile(),
		testWASI2HostProfile(m.dispatcher),
	))
}

func testWASI1HostProfile() wasmcomponent.HostProfile {
	return wasmcomponent.HostProfile{
		Name: wasmcomponent.HostProfileWASI1,
		Aliases: []string{
			wasmcomponent.HostProfileWASI1,
			"wasi-preview1",
			"preview1",
			"wasi_snapshot_preview1",
		},
	}
}

func testFuncsHostProfile() wasmcomponent.HostProfile {
	return wasmcomponent.HostProfile{
		Name:          wasmcomponent.HostProfileFuncs,
		ComponentOnly: true,
		Aliases: []string{
			wasmcomponent.HostProfileFuncs,
			"wippy:runtime/funcs",
		},
		Register: func(ctx context.Context, _ *wasmrt.Runtime) error {
			if functionapi.GetRegistry(ctx) == nil {
				return runtimewasm.ErrFunctionRegistryNotFound
			}
			return nil
		},
	}
}

func testWASI2HostProfile(d dispatcher.Dispatcher) wasmcomponent.HostProfile {
	return wasmcomponent.HostProfile{
		Name:          wasmcomponent.HostProfileWASI2,
		ComponentOnly: true,
		Aliases: []string{
			wasmcomponent.HostProfileWASI2,
			"wasi-preview2",
			"preview2",
			"wasi:clocks/wall-clock@0.2.3",
			"wasi:clocks/monotonic-clock@0.2.8",
			"wasi:io/poll@0.2.8",
			"wasi:io/error@0.2.8",
			"wasi:io/streams@0.2.8",
			"wasi:cli/environment@0.2.3",
			"wasi:cli/exit@0.2.3",
			"wasi:cli/stdin@0.2.3",
			"wasi:cli/stdout@0.2.3",
			"wasi:cli/stderr@0.2.3",
			"wasi:cli/terminal-stdin@0.2.3",
			"wasi:cli/terminal-stdout@0.2.3",
			"wasi:cli/terminal-stderr@0.2.3",
			"wasi:filesystem/types@0.2.3",
			"wasi:filesystem/preopens@0.2.3",
			"wasi:random/random@0.2.0",
			"wasi:random/insecure@0.2.0",
			"wasi:random/insecure-seed@0.2.0",
			"wasi:sockets/instance-network@0.2.0",
			"wasi:sockets/tcp-create-socket@0.2.0",
			"wasi:sockets/tcp@0.2.0",
			"wasi:sockets/udp-create-socket@0.2.0",
			"wasi:sockets/udp@0.2.0",
			"wasi:sockets/ip-name-lookup@0.2.0",
			"wasi:http/types@0.2.8",
			"wasi:http/outgoing-handler@0.2.8",
			"wasi:clocks/wall-clock",
			"wasi:clocks/monotonic-clock",
			"wasi:io/poll",
			"wasi:io/error",
			"wasi:io/streams",
			"wasi:cli/environment",
			"wasi:cli/exit",
			"wasi:cli/stdin",
			"wasi:cli/stdout",
			"wasi:cli/stderr",
			"wasi:cli/terminal-stdin",
			"wasi:cli/terminal-stdout",
			"wasi:cli/terminal-stderr",
			"wasi:filesystem/types",
			"wasi:filesystem/preopens",
			"wasi:random/random",
			"wasi:random/insecure",
			"wasi:random/insecure-seed",
			"wasi:sockets/instance-network",
			"wasi:sockets/tcp-create-socket",
			"wasi:sockets/tcp",
			"wasi:sockets/udp-create-socket",
			"wasi:sockets/udp",
			"wasi:sockets/ip-name-lookup",
			"wasi:http/types",
			"wasi:http/outgoing-handler",
		},
		Register: func(_ context.Context, rt *wasmrt.Runtime) error {
			if d == nil {
				return runtimewasm.ErrDispatcherNotFound
			}
			return nil
		},
	}
}

func TestLoadWATModule_NoImplicitHostImports(t *testing.T) {
	ctx := ctxapi.NewRootContext()

	m := NewManager(zap.NewNop(), nil, nil, nil)
	require.NoError(t, m.Start(ctx))
	t.Cleanup(m.Stop)

	cfg := &wasmapi.WATFunctionConfig{
		Source: `(module
			(func (export "run") (result i32)
				i32.const 7
			)
		)`,
		Method: "run",
	}

	mod, err := m.loadWATModule(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, mod)
}

func TestLoadWATModule_ComponentOnlyImportRejected(t *testing.T) {
	ctx := ctxapi.NewRootContext()

	m := NewManager(zap.NewNop(), nil, nil, nil)
	registerDefaultHostProfiles(t, m)
	require.NoError(t, m.Start(ctx))
	t.Cleanup(m.Stop)

	cfg := &wasmapi.WATFunctionConfig{
		Source: `(module
			(func (export "run") (result i32)
				i32.const 7
			)
		)`,
		Method: "run",
		Imports: []registry.ID{
			registry.ParseID("funcs"),
		},
	}

	_, err := m.loadWATModule(ctx, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires component module")
}

func TestEnsureImportHosts_FuncsRequiresRegistry(t *testing.T) {
	ctx := ctxapi.NewRootContext()

	m := NewManager(zap.NewNop(), nil, nil, nil)
	registerDefaultHostProfiles(t, m)
	require.NoError(t, m.Start(ctx))
	t.Cleanup(m.Stop)

	err := m.ensureImportHosts(ctx, []registry.ID{
		registry.ParseID("funcs"),
	}, true)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "function registry not found")
}

func TestEnsureImportHosts_FuncsRegistersOnce(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = functionapi.WithRegistry(ctx, noopFunctionRegistry{})

	m := NewManager(zap.NewNop(), nil, nil, nil)
	registerDefaultHostProfiles(t, m)
	require.NoError(t, m.Start(ctx))
	t.Cleanup(m.Stop)

	imports := []registry.ID{
		registry.ParseID("funcs"),
	}

	require.NoError(t, m.ensureImportHosts(ctx, imports, true))
	require.NoError(t, m.ensureImportHosts(ctx, imports, true))
}

func TestEnsureImportHosts_WASI2RegistersOnce(t *testing.T) {
	ctx := ctxapi.NewRootContext()

	m := NewManager(zap.NewNop(), nil, noopDispatcher{}, nil)
	registerDefaultHostProfiles(t, m)
	require.NoError(t, m.Start(ctx))
	t.Cleanup(m.Stop)

	imports := []registry.ID{
		registry.ParseID("wasi2"),
	}

	require.NoError(t, m.ensureImportHosts(ctx, imports, true))
	require.NoError(t, m.ensureImportHosts(ctx, imports, true))
}

func TestEnsureImportHosts_WASI2RequiresDispatcher(t *testing.T) {
	ctx := ctxapi.NewRootContext()

	m := NewManager(zap.NewNop(), nil, nil, nil)
	registerDefaultHostProfiles(t, m)
	require.NoError(t, m.Start(ctx))
	t.Cleanup(m.Stop)

	err := m.ensureImportHosts(ctx, []registry.ID{registry.ParseID("wasi2")}, true)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "dispatcher not found")
}

func TestEnsureImportHosts_EmptyNoRegistration(t *testing.T) {
	ctx := ctxapi.NewRootContext()

	m := NewManager(zap.NewNop(), nil, nil, nil)
	var calls int32
	require.NoError(t, m.RegisterHostProfiles(wasmcomponent.HostProfile{
		Name:          "test-empty",
		ComponentOnly: true,
		Register: func(context.Context, *wasmrt.Runtime) error {
			atomic.AddInt32(&calls, 1)
			return nil
		},
	}))
	require.NoError(t, m.Start(ctx))
	t.Cleanup(m.Stop)

	require.NoError(t, m.ensureImportHosts(ctx, nil, true))
	require.NoError(t, m.ensureImportHosts(ctx, []registry.ID{}, true))
	assert.Equal(t, int32(0), atomic.LoadInt32(&calls))
}

func TestEnsureImportHosts_RegistersOnceAcrossConcurrentLoads(t *testing.T) {
	ctx := ctxapi.NewRootContext()

	m := NewManager(zap.NewNop(), nil, nil, nil)
	var calls int32
	require.NoError(t, m.RegisterHostProfiles(wasmcomponent.HostProfile{
		Name:          "test-concurrent",
		ComponentOnly: true,
		Aliases:       []string{"test:host/concurrent"},
		Register: func(context.Context, *wasmrt.Runtime) error {
			atomic.AddInt32(&calls, 1)
			return nil
		},
	}))
	require.NoError(t, m.Start(ctx))
	t.Cleanup(m.Stop)

	imports := []registry.ID{
		registry.ParseID("test-concurrent"),
		registry.ParseID("test:host/concurrent@0.1.0"),
	}

	const workers = 16
	errCh := make(chan error, workers)
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- m.ensureImportHosts(ctx, imports, true)
		}()
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}

	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
}

func TestResolveHostProfile(t *testing.T) {
	m := NewManager(zap.NewNop(), nil, nil, nil)
	registerDefaultHostProfiles(t, m)

	tests := []struct {
		name   string
		id     registry.ID
		want   string
		wantOK bool
	}{
		{name: "funcs short", id: registry.ParseID("funcs"), want: wasmcomponent.HostProfileFuncs, wantOK: true},
		{name: "funcs versioned", id: registry.ParseID("funcs@0.1.0"), want: wasmcomponent.HostProfileFuncs, wantOK: true},
		{name: "funcs namespace", id: registry.ParseID("wippy:runtime/funcs@0.1.0"), want: wasmcomponent.HostProfileFuncs, wantOK: true},
		{name: "wasi1 short", id: registry.ParseID("wasi1"), want: wasmcomponent.HostProfileWASI1, wantOK: true},
		{name: "wasi preview1", id: registry.ParseID("wasi_snapshot_preview1"), want: wasmcomponent.HostProfileWASI1, wantOK: true},
		{name: "wasi2 short", id: registry.ParseID("wasi2"), want: wasmcomponent.HostProfileWASI2, wantOK: true},
		{name: "wasi2 versioned", id: registry.ParseID("wasi2@0.2.8"), want: wasmcomponent.HostProfileWASI2, wantOK: true},
		{name: "wasi2 canonical poll", id: registry.ParseID("wasi:io/poll@0.2.8"), want: wasmcomponent.HostProfileWASI2, wantOK: true},
		{name: "wasi2 canonical env", id: registry.ParseID("wasi:cli/environment@0.2.3"), want: wasmcomponent.HostProfileWASI2, wantOK: true},
		{name: "wasi2 short env", id: registry.ParseID("wasi:cli/environment"), want: wasmcomponent.HostProfileWASI2, wantOK: true},
		{name: "wasi2 canonical fs types", id: registry.ParseID("wasi:filesystem/types@0.2.3"), want: wasmcomponent.HostProfileWASI2, wantOK: true},
		{name: "wasi2 canonical http outgoing", id: registry.ParseID("wasi:http/outgoing-handler@0.2.8"), want: wasmcomponent.HostProfileWASI2, wantOK: true},
		{name: "wasi2 canonical cli stdout", id: registry.ParseID("wasi:cli/stdout@0.2.3"), want: wasmcomponent.HostProfileWASI2, wantOK: true},
		{name: "wasi2 canonical preopens", id: registry.ParseID("wasi:filesystem/preopens@0.2.3"), want: wasmcomponent.HostProfileWASI2, wantOK: true},
		{name: "wasi2 canonical sockets tcp", id: registry.ParseID("wasi:sockets/tcp@0.2.0"), want: wasmcomponent.HostProfileWASI2, wantOK: true},
		{name: "wasi2 incoming handler is export-side", id: registry.ParseID("wasi:http/incoming-handler@0.2.8"), want: "", wantOK: false},
		{name: "wasi2 cli run is export-side", id: registry.ParseID("wasi:cli/run@0.2.3"), want: "", wantOK: false},
		{name: "unknown", id: registry.ParseID("custom-host"), want: "", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := m.hostRegistry.Resolve(tt.id)
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.Equal(t, tt.want, got.Name)
			}
		})
	}
}

var _ functionapi.Registry = noopFunctionRegistry{}
