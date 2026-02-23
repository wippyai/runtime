// SPDX-License-Identifier: MPL-2.0

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
	profiles := []wasmcomponent.HostProfile{
		testHostProfile(),
		testWASI1HostProfile(),
	}
	profiles = append(profiles, testGranularWASIProfiles(m.dispatcher)...)
	require.NoError(t, m.RegisterHostProfiles(profiles...))
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

func testHostProfile() wasmcomponent.HostProfile {
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

func testGranularWASIProfiles(d dispatcher.Dispatcher) []wasmcomponent.HostProfile {
	return []wasmcomponent.HostProfile{
		{
			Name:          wasmcomponent.HostProfileWASIIO,
			ComponentOnly: true,
			Aliases: []string{
				wasmcomponent.HostProfileWASIIO,
				"wasi:io/error",
				"wasi:io/streams",
			},
			Register: func(_ context.Context, _ *wasmrt.Runtime) error { return nil },
		},
		{
			Name:          wasmcomponent.HostProfileWASIPoll,
			ComponentOnly: true,
			Aliases: []string{
				wasmcomponent.HostProfileWASIPoll,
				"wasi:io/poll",
			},
			Register: func(_ context.Context, _ *wasmrt.Runtime) error { return nil },
		},
		{
			Name:          wasmcomponent.HostProfileWASIClocks,
			ComponentOnly: true,
			Aliases: []string{
				wasmcomponent.HostProfileWASIClocks,
				"wasi:clocks/wall-clock",
				"wasi:clocks/monotonic-clock",
			},
			Register: func(_ context.Context, _ *wasmrt.Runtime) error { return nil },
		},
		{
			Name:          wasmcomponent.HostProfileWASICLI,
			ComponentOnly: true,
			Aliases: []string{
				wasmcomponent.HostProfileWASICLI,
				"wasi:cli/environment",
				"wasi:cli/exit",
				"wasi:cli/stdin",
				"wasi:cli/stdout",
				"wasi:cli/stderr",
				"wasi:cli/terminal-stdin",
				"wasi:cli/terminal-stdout",
				"wasi:cli/terminal-stderr",
			},
			Register: func(_ context.Context, _ *wasmrt.Runtime) error { return nil },
		},
		{
			Name:          wasmcomponent.HostProfileWASIFilesystem,
			ComponentOnly: true,
			Aliases: []string{
				wasmcomponent.HostProfileWASIFilesystem,
				"wasi:filesystem/types",
				"wasi:filesystem/preopens",
			},
			Register: func(_ context.Context, _ *wasmrt.Runtime) error { return nil },
		},
		{
			Name:          wasmcomponent.HostProfileWASIRandom,
			ComponentOnly: true,
			Aliases: []string{
				wasmcomponent.HostProfileWASIRandom,
				"wasi:random/random",
				"wasi:random/insecure",
				"wasi:random/insecure-seed",
			},
			Register: func(_ context.Context, _ *wasmrt.Runtime) error { return nil },
		},
		{
			Name:          wasmcomponent.HostProfileWASISockets,
			ComponentOnly: true,
			Aliases: []string{
				wasmcomponent.HostProfileWASISockets,
				"wasi:sockets/instance-network",
				"wasi:sockets/tcp-create-socket",
				"wasi:sockets/tcp",
				"wasi:sockets/udp-create-socket",
				"wasi:sockets/udp",
				"wasi:sockets/ip-name-lookup",
			},
			Register: func(_ context.Context, _ *wasmrt.Runtime) error { return nil },
		},
		{
			Name:          wasmcomponent.HostProfileWASIHTTP,
			ComponentOnly: true,
			Aliases: []string{
				wasmcomponent.HostProfileWASIHTTP,
				"wasi:http/types",
				"wasi:http/outgoing-handler",
			},
			Register: func(_ context.Context, _ *wasmrt.Runtime) error {
				if d == nil {
					return runtimewasm.ErrDispatcherNotFound
				}
				return nil
			},
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

func TestEnsureImportHosts_GranularProfileRegistersOnce(t *testing.T) {
	ctx := ctxapi.NewRootContext()

	m := NewManager(zap.NewNop(), nil, noopDispatcher{}, nil)
	registerDefaultHostProfiles(t, m)
	require.NoError(t, m.Start(ctx))
	t.Cleanup(m.Stop)

	imports := []registry.ID{
		registry.ParseID("wasi:clocks"),
	}

	require.NoError(t, m.ensureImportHosts(ctx, imports, true))
	require.NoError(t, m.ensureImportHosts(ctx, imports, true))
}

func TestEnsureImportHosts_HTTPRequiresDispatcher(t *testing.T) {
	ctx := ctxapi.NewRootContext()

	m := NewManager(zap.NewNop(), nil, nil, nil)
	registerDefaultHostProfiles(t, m)
	require.NoError(t, m.Start(ctx))
	t.Cleanup(m.Stop)

	err := m.ensureImportHosts(ctx, []registry.ID{registry.ParseID("wasi:http")}, true)
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
		// funcs
		{name: "funcs short", id: registry.ParseID("funcs"), want: wasmcomponent.HostProfileFuncs, wantOK: true},
		{name: "funcs versioned", id: registry.ParseID("funcs@0.1.0"), want: wasmcomponent.HostProfileFuncs, wantOK: true},
		{name: "funcs namespace", id: registry.ParseID("wippy:runtime/funcs@0.1.0"), want: wasmcomponent.HostProfileFuncs, wantOK: true},
		// wasi1
		{name: "wasi1 short", id: registry.ParseID("wasi1"), want: wasmcomponent.HostProfileWASI1, wantOK: true},
		{name: "wasi preview1", id: registry.ParseID("wasi_snapshot_preview1"), want: wasmcomponent.HostProfileWASI1, wantOK: true},
		// wasi:io
		{name: "wasi:io short", id: registry.ParseID("wasi:io"), want: wasmcomponent.HostProfileWASIIO, wantOK: true},
		{name: "wasi:io/error", id: registry.ParseID("wasi:io/error@0.2.8"), want: wasmcomponent.HostProfileWASIIO, wantOK: true},
		{name: "wasi:io/streams", id: registry.ParseID("wasi:io/streams"), want: wasmcomponent.HostProfileWASIIO, wantOK: true},
		// wasi:poll
		{name: "wasi:poll short", id: registry.ParseID("wasi:poll"), want: wasmcomponent.HostProfileWASIPoll, wantOK: true},
		{name: "wasi:io/poll canonical", id: registry.ParseID("wasi:io/poll@0.2.8"), want: wasmcomponent.HostProfileWASIPoll, wantOK: true},
		// wasi:clocks
		{name: "wasi:clocks short", id: registry.ParseID("wasi:clocks"), want: wasmcomponent.HostProfileWASIClocks, wantOK: true},
		{name: "wasi:clocks/wall-clock", id: registry.ParseID("wasi:clocks/wall-clock@0.2.3"), want: wasmcomponent.HostProfileWASIClocks, wantOK: true},
		{name: "wasi:clocks/monotonic-clock", id: registry.ParseID("wasi:clocks/monotonic-clock"), want: wasmcomponent.HostProfileWASIClocks, wantOK: true},
		// wasi:cli
		{name: "wasi:cli short", id: registry.ParseID("wasi:cli"), want: wasmcomponent.HostProfileWASICLI, wantOK: true},
		{name: "wasi:cli/environment", id: registry.ParseID("wasi:cli/environment@0.2.3"), want: wasmcomponent.HostProfileWASICLI, wantOK: true},
		{name: "wasi:cli/exit", id: registry.ParseID("wasi:cli/exit@0.2.3"), want: wasmcomponent.HostProfileWASICLI, wantOK: true},
		{name: "wasi:cli/stdout", id: registry.ParseID("wasi:cli/stdout@0.2.3"), want: wasmcomponent.HostProfileWASICLI, wantOK: true},
		{name: "wasi:cli/stderr", id: registry.ParseID("wasi:cli/stderr"), want: wasmcomponent.HostProfileWASICLI, wantOK: true},
		{name: "wasi:cli/terminal-stdin", id: registry.ParseID("wasi:cli/terminal-stdin"), want: wasmcomponent.HostProfileWASICLI, wantOK: true},
		// wasi:filesystem
		{name: "wasi:filesystem short", id: registry.ParseID("wasi:filesystem"), want: wasmcomponent.HostProfileWASIFilesystem, wantOK: true},
		{name: "wasi:filesystem/types", id: registry.ParseID("wasi:filesystem/types@0.2.3"), want: wasmcomponent.HostProfileWASIFilesystem, wantOK: true},
		{name: "wasi:filesystem/preopens", id: registry.ParseID("wasi:filesystem/preopens@0.2.3"), want: wasmcomponent.HostProfileWASIFilesystem, wantOK: true},
		// wasi:random
		{name: "wasi:random short", id: registry.ParseID("wasi:random"), want: wasmcomponent.HostProfileWASIRandom, wantOK: true},
		{name: "wasi:random/random", id: registry.ParseID("wasi:random/random@0.2.0"), want: wasmcomponent.HostProfileWASIRandom, wantOK: true},
		{name: "wasi:random/insecure", id: registry.ParseID("wasi:random/insecure"), want: wasmcomponent.HostProfileWASIRandom, wantOK: true},
		// wasi:sockets
		{name: "wasi:sockets short", id: registry.ParseID("wasi:sockets"), want: wasmcomponent.HostProfileWASISockets, wantOK: true},
		{name: "wasi:sockets/tcp", id: registry.ParseID("wasi:sockets/tcp@0.2.0"), want: wasmcomponent.HostProfileWASISockets, wantOK: true},
		{name: "wasi:sockets/udp", id: registry.ParseID("wasi:sockets/udp"), want: wasmcomponent.HostProfileWASISockets, wantOK: true},
		{name: "wasi:sockets/ip-name-lookup", id: registry.ParseID("wasi:sockets/ip-name-lookup"), want: wasmcomponent.HostProfileWASISockets, wantOK: true},
		// wasi:http
		{name: "wasi:http short", id: registry.ParseID("wasi:http"), want: wasmcomponent.HostProfileWASIHTTP, wantOK: true},
		{name: "wasi:http/types", id: registry.ParseID("wasi:http/types@0.2.8"), want: wasmcomponent.HostProfileWASIHTTP, wantOK: true},
		{name: "wasi:http/outgoing-handler", id: registry.ParseID("wasi:http/outgoing-handler@0.2.8"), want: wasmcomponent.HostProfileWASIHTTP, wantOK: true},
		// export-side (not imported)
		{name: "wasi:http/incoming-handler is export-side", id: registry.ParseID("wasi:http/incoming-handler@0.2.8"), want: "", wantOK: false},
		{name: "wasi:cli/run is export-side", id: registry.ParseID("wasi:cli/run@0.2.3"), want: "", wantOK: false},
		// unknown
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

func TestSharedResourceTable(t *testing.T) {
	reg := wasmcomponent.NewHostRegistry()

	assert.Nil(t, reg.SharedResources())

	type fakeTable struct{ id int }
	table := &fakeTable{id: 42}
	reg.SetSharedResources(table)

	got := reg.SharedResources()
	require.NotNil(t, got)
	assert.Equal(t, 42, got.(*fakeTable).id)

	reg.ResetLoaded()
	assert.Nil(t, reg.SharedResources())
}

func TestHostRegistryContext(t *testing.T) {
	ctx := context.Background()
	assert.Nil(t, wasmcomponent.GetHostRegistry(ctx))

	reg := wasmcomponent.NewHostRegistry()
	ctx = wasmcomponent.WithHostRegistry(ctx, reg)
	assert.Equal(t, reg, wasmcomponent.GetHostRegistry(ctx))
}

var _ functionapi.Registry = noopFunctionRegistry{}
