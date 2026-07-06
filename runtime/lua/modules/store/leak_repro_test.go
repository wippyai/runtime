// SPDX-License-Identifier: MPL-2.0

package store

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"sync/atomic"
	"testing"

	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	apiresource "github.com/wippyai/runtime/api/resource"
	rtresource "github.com/wippyai/runtime/api/runtime/resource"
	"github.com/wippyai/runtime/api/security"
	storeapi "github.com/wippyai/runtime/api/store"
)

type leakReproRegistry struct {
	store    storeapi.Store
	acquires atomic.Int64
	releases atomic.Int64
}

func newLeakReproRegistry(s storeapi.Store) *leakReproRegistry {
	return &leakReproRegistry{store: s}
}

func (r *leakReproRegistry) Acquire(_ context.Context, id registry.ID, _ apiresource.AccessMode) (apiresource.Resource[any], error) {
	if id.String() != "test:mystore" {
		return nil, apiresource.ErrNotFound
	}
	r.acquires.Add(1)
	return &leakReproResource{store: r.store, releases: &r.releases}, nil
}

func (r *leakReproRegistry) List() ([]registry.ID, error) {
	return []registry.ID{registry.ParseID("test:mystore")}, nil
}

func (r *leakReproRegistry) Exists(id registry.ID) bool {
	return id.String() == "test:mystore"
}

type leakReproResource struct {
	store    storeapi.Store
	releases *atomic.Int64
	released atomic.Bool
}

func (r *leakReproResource) Get() (any, error) {
	if r.released.Load() {
		return nil, apiresource.ErrReleased
	}
	return r.store, nil
}

func (r *leakReproResource) Release() {
	if r.released.CompareAndSwap(false, true) {
		r.releases.Add(1)
	}
}

func setupLeakReproState(t *testing.T, reg apiresource.Registry) (*lua.LState, *rtresource.Store) {
	t.Helper()

	l := setupState()
	ctx, fc := ctxapi.OpenFrameContext(ctxapi.NewRootContext())
	ctx = security.SetStrictMode(ctx, false)
	ctx = apiresource.WithRegistry(ctx, reg)
	ctx = payload.WithTranscoder(ctx, &mockTranscoder{})

	resStore := rtresource.NewStore()
	if err := rtresource.SetStore(ctx, resStore); err != nil {
		t.Fatalf("set runtime resource store: %v", err)
	}

	l.SetContext(ctx)
	t.Cleanup(func() {
		_ = resStore.Close()
		ctxapi.ReleaseFrameContext(fc)
		l.Close()
	})

	return l, resStore
}

func runtimeStoreLiveCleanups(s *rtresource.Store) int {
	v := reflect.ValueOf(s).Elem().FieldByName("count")
	return int(v.Int())
}

func TestStoreGetWithoutReleaseReproducesPerCallResourceLeak(t *testing.T) {
	const calls = 50000

	reg := newLeakReproRegistry(newMemoryStore())
	l, resStore := setupLeakReproState(t, reg)

	if err := l.DoString(fmt.Sprintf(`
		for i = 1, %d do
			local s, err = store.get("test:mystore")
			if err then error(tostring(err)) end
			if s == nil then error("store.get returned nil") end
		end
	`, calls)); err != nil {
		t.Fatalf("store.get loop failed: %v", err)
	}

	runtime.GC()

	acquires := int(reg.acquires.Load())
	releases := int(reg.releases.Load())
	liveCleanups := runtimeStoreLiveCleanups(resStore)

	t.Logf("calls=%d registry_acquires=%d registry_releases=%d live_resource_cleanups=%d",
		calls, acquires, releases, liveCleanups)

	if acquires > 1 || liveCleanups > 1 {
		t.Fatalf("reproduced per-call retained resource leak: calls=%d registry_acquires=%d registry_releases=%d live_resource_cleanups=%d; want acquires and live cleanups bounded",
			calls, acquires, releases, liveCleanups)
	}
}

func TestStoreGetLeasedHandlesReleaseIndependently(t *testing.T) {
	reg := newLeakReproRegistry(newMemoryStore())
	l, resStore := setupLeakReproState(t, reg)

	if err := l.DoString(`
		local a, err = store.get("test:mystore")
		if err then error(tostring(err)) end
		local b, err = store.get("test:mystore")
		if err then error(tostring(err)) end

		a:release()

		local info, info_err = b:info()
		if info_err then error(tostring(info_err)) end
		if info.id ~= "test:mystore" then error("wrong store id: " .. tostring(info.id)) end

		b:release()
	`); err != nil {
		t.Fatalf("independent release script failed: %v", err)
	}

	if acquires := reg.acquires.Load(); acquires != 1 {
		t.Fatalf("registry acquires = %d, want 1", acquires)
	}
	if releases := reg.releases.Load(); releases != 1 {
		t.Fatalf("registry releases = %d, want 1 after last handle release", releases)
	}
	if liveCleanups := runtimeStoreLiveCleanups(resStore); liveCleanups != 0 {
		t.Fatalf("live resource cleanups = %d, want 0", liveCleanups)
	}
}

func TestStoreGetLeasedHandlesReleaseOnProcessResourceClose(t *testing.T) {
	reg := newLeakReproRegistry(newMemoryStore())
	l, resStore := setupLeakReproState(t, reg)

	if err := l.DoString(`
		for i = 1, 100 do
			local s, err = store.get("test:mystore")
			if err then error(tostring(err)) end
			if s == nil then error("store.get returned nil") end
		end
	`); err != nil {
		t.Fatalf("store.get loop failed: %v", err)
	}

	if acquires := reg.acquires.Load(); acquires != 1 {
		t.Fatalf("registry acquires before close = %d, want 1", acquires)
	}
	if releases := reg.releases.Load(); releases != 0 {
		t.Fatalf("registry releases before close = %d, want 0", releases)
	}

	if err := resStore.Close(); err != nil {
		t.Fatalf("resource store close: %v", err)
	}
	if releases := reg.releases.Load(); releases != 1 {
		t.Fatalf("registry releases after close = %d, want 1", releases)
	}
}
