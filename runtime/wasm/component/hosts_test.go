package component

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
)

func TestHostRegistryResolve(t *testing.T) {
	r := NewHostRegistry()
	require.NoError(t, r.RegisterProfiles(
		HostProfile{Name: "funcs", Aliases: []string{"wippy:runtime/funcs@0.1.0"}},
		HostProfile{Name: "wasi2", Aliases: []string{"wasi:io/poll@0.2.8", "wasi:filesystem/types@0.2.3"}},
	))

	cases := []struct {
		name   string
		id     registry.ID
		want   string
		wantOK bool
	}{
		{name: "short", id: registry.ParseID("funcs"), want: "funcs", wantOK: true},
		{name: "canonical versioned", id: registry.ParseID("wippy:runtime/funcs@0.1.0"), want: "funcs", wantOK: true},
		{name: "alias stripped version", id: registry.ParseID("wasi:io/poll@0.2.9"), want: "wasi2", wantOK: true},
		{name: "fs types short", id: registry.ParseID("wasi:filesystem/types"), want: "wasi2", wantOK: true},
		{name: "unknown", id: registry.ParseID("unknown"), wantOK: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := r.Resolve(tc.id)
			assert.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				assert.Equal(t, tc.want, got.Name)
			}
		})
	}
}

func TestHostRegistryEnsureImports_EmptyNoop(t *testing.T) {
	r := NewHostRegistry()
	require.NoError(t, r.EnsureImports(context.Background(), nil, nil, true))
	require.NoError(t, r.EnsureImports(context.Background(), nil, []registry.ID{}, true))
}

func TestHostRegistryEnsureImports_ComponentOnlyRejectedForCore(t *testing.T) {
	r := NewHostRegistry()
	require.NoError(t, r.RegisterProfiles(HostProfile{
		Name:          "component-only",
		ComponentOnly: true,
	}))

	err := r.EnsureImports(context.Background(), nil, []registry.ID{registry.ParseID("component-only")}, false)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "requires component module")
}

func TestHostRegistryEnsureImports_UnknownImportRejected(t *testing.T) {
	r := NewHostRegistry()
	err := r.EnsureImports(context.Background(), nil, []registry.ID{registry.ParseID("does-not-exist")}, true)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "unsupported wasm host import")
}

func TestHostRegistryEnsureImports_RegistersOnceAcrossConcurrentLoads(t *testing.T) {
	r := NewHostRegistry()
	var calls int32

	require.NoError(t, r.RegisterProfiles(HostProfile{
		Name:          "test-concurrent",
		ComponentOnly: true,
		Aliases:       []string{"test:host/concurrent"},
		Register: func(context.Context, *wasmrt.Runtime) error {
			atomic.AddInt32(&calls, 1)
			return nil
		},
	}))

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
			errCh <- r.EnsureImports(context.Background(), &wasmrt.Runtime{}, imports, true)
		}()
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}

	assert.Equal(t, int32(1), atomic.LoadInt32(&calls))
}
