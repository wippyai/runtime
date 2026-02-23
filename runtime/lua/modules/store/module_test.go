// SPDX-License-Identifier: MPL-2.0

package store

import (
	"context"
	"sync"
	"testing"

	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/api/store"
)

// memoryStore is a simple in-memory store for testing
type memoryStore struct {
	data map[string]payload.Payload
	mu   sync.RWMutex
}

func newMemoryStore() *memoryStore {
	return &memoryStore{data: make(map[string]payload.Payload)}
}

func (s *memoryStore) Get(_ context.Context, key registry.ID) (payload.Payload, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[key.String()]
	if !ok {
		return nil, store.ErrKeyNotFound
	}
	return v, nil
}

func (s *memoryStore) Set(_ context.Context, entry store.Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[entry.Key.String()] = entry.Value
	return nil
}

func (s *memoryStore) Delete(_ context.Context, key registry.ID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[key.String()]; !ok {
		return store.ErrKeyNotFound
	}
	delete(s.data, key.String())
	return nil
}

func (s *memoryStore) Has(_ context.Context, key registry.ID) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.data[key.String()]
	return ok, nil
}

// mockResource wraps a store for resource acquisition
type mockResource struct {
	store    store.Store
	released bool
	mu       sync.Mutex
}

func (r *mockResource) Get() (any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.released {
		return nil, resource.ErrReleased
	}
	return r.store, nil
}

func (r *mockResource) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.released = true
}

// mockRegistry provides resources for testing
type mockRegistry struct {
	stores map[string]store.Store
	mu     sync.RWMutex
}

func newMockRegistry() *mockRegistry {
	return &mockRegistry{stores: make(map[string]store.Store)}
}

func (r *mockRegistry) Register(id string, s store.Store) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stores[id] = s
}

func (r *mockRegistry) Acquire(_ context.Context, id registry.ID, _ resource.AccessMode) (resource.Resource[any], error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.stores[id.String()]
	if !ok {
		return nil, resource.ErrNotFound
	}
	return &mockResource{store: s}, nil
}

func (r *mockRegistry) List() ([]registry.ID, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]registry.ID, 0, len(r.stores))
	for k := range r.stores {
		ids = append(ids, registry.ParseID(k))
	}
	return ids, nil
}

func (r *mockRegistry) Exists(id registry.ID) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.stores[id.String()]
	return ok
}

// mockTranscoder passes through payloads
type mockTranscoder struct{}

func (t *mockTranscoder) Transcode(p payload.Payload, _ payload.Format) (payload.Payload, error) {
	return p, nil
}

func (t *mockTranscoder) Unmarshal(_ payload.Payload, _ any) error {
	return nil
}

func setupState() *lua.LState {
	l := lua.NewState()
	lua.OpenErrors(l)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)
	return l
}

func setupStateWithContext(reg *mockRegistry) *lua.LState {
	l := lua.NewState()
	lua.OpenErrors(l)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	ctx := ctxapi.NewRootContext()
	ctx = security.SetStrictMode(ctx, false)
	ctx = resource.WithRegistry(ctx, reg)
	ctx = payload.WithTranscoder(ctx, &mockTranscoder{})
	l.SetContext(ctx)

	return l
}

func TestModuleLoads(t *testing.T) {
	l := setupState()
	defer l.Close()

	mod := l.GetGlobal("store")
	if mod.Type() != lua.LTTable {
		t.Fatal("store module not registered")
	}

	modTbl := mod.(*lua.LTable)
	if modTbl.RawGetString("get").Type() != lua.LTFunction {
		t.Error("get function not registered")
	}
}

func TestModuleReuse(t *testing.T) {
	l1 := lua.NewState()
	defer l1.Close()
	l2 := lua.NewState()
	defer l2.Close()

	tbl, _ := Module.Build()
	l1.SetGlobal(Module.Name, tbl)
	l2.SetGlobal(Module.Name, tbl)

	mod1 := l1.GetGlobal("store").(*lua.LTable)
	mod2 := l2.GetGlobal("store").(*lua.LTable)

	if mod1 != mod2 {
		t.Error("module table should be reused across states")
	}
}

func TestModuleImmutable(t *testing.T) {
	l := setupState()
	defer l.Close()

	mod := l.GetGlobal("store").(*lua.LTable)
	if !mod.Immutable {
		t.Error("module table should be immutable")
	}
}

func TestStoreGetNoAppContext(t *testing.T) {
	l := setupState()
	defer l.Close()

	ctx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	l.SetContext(ctx)

	err := l.DoString(`
		local s, err = store.get("test:store")
		if not err then
			error("expected error when no app context")
		end
		if err:kind() ~= errors.NOT_FOUND then
			error("expected NOT_FOUND error kind, got: " .. tostring(err:kind()))
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestStoreGetEmptyID(t *testing.T) {
	l := setupState()
	defer l.Close()
	ctx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	l.SetContext(ctx)

	err := l.DoString(`
		local s, err = store.get("")
		if not err then
			error("expected error for empty ID")
		end
		if err:kind() ~= errors.INVALID then
			error("expected INVALID error kind")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestStoreGetNoRegistry(t *testing.T) {
	l := setupState()
	defer l.Close()

	ctx := ctxapi.NewRootContext()
	ctx = security.SetStrictMode(ctx, false)
	l.SetContext(ctx)

	err := l.DoString(`
		local s, err = store.get("test:nonexistent")
		if not err then
			error("expected error for no registry")
		end
		if err:kind() ~= errors.NOT_FOUND then
			error("expected NOT_FOUND error kind, got: " .. tostring(err:kind()))
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestStoreGetNotFound(t *testing.T) {
	reg := newMockRegistry()
	l := setupStateWithContext(reg)
	defer l.Close()

	err := l.DoString(`
		local s, err = store.get("test:nonexistent")
		if not err then
			error("expected error for nonexistent store")
		end
		if err:kind() ~= errors.INTERNAL then
			error("expected INTERNAL error kind, got: " .. tostring(err:kind()))
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestStoreGetSuccess(t *testing.T) {
	reg := newMockRegistry()
	mem := newMemoryStore()
	reg.Register("test:mystore", mem)

	l := setupStateWithContext(reg)
	defer l.Close()

	err := l.DoString(`
		local s, err = store.get("test:mystore")
		if err then
			error("unexpected error: " .. tostring(err))
		end
		if s == nil then
			error("expected store object")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestStoreToString(t *testing.T) {
	reg := newMockRegistry()
	mem := newMemoryStore()
	reg.Register("test:mystore", mem)

	l := setupStateWithContext(reg)
	defer l.Close()

	err := l.DoString(`
		local s = store.get("test:mystore")
		local str = tostring(s)
		if str ~= "store.Store{}" then
			error("expected 'store.Store{}', got: " .. str)
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestStoreRelease(t *testing.T) {
	reg := newMockRegistry()
	mem := newMemoryStore()
	reg.Register("test:mystore", mem)

	l := setupStateWithContext(reg)
	defer l.Close()

	err := l.DoString(`
		local s = store.get("test:mystore")
		local ok = s:release()
		if not ok then
			error("release should return true")
		end
		local str = tostring(s)
		if str ~= "store.Store{released}" then
			error("expected 'store.Store{released}', got: " .. str)
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestStoreReleaseIdempotent(t *testing.T) {
	reg := newMockRegistry()
	mem := newMemoryStore()
	reg.Register("test:mystore", mem)

	l := setupStateWithContext(reg)
	defer l.Close()

	err := l.DoString(`
		local s = store.get("test:mystore")
		s:release()
		local ok = s:release()
		if not ok then
			error("second release should also return true")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestErrorKinds(t *testing.T) {
	l := setupState()
	defer l.Close()
	ctx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	l.SetContext(ctx)

	err := l.DoString(`
		local s, err = store.get("")
		if not err then error("expected error") end

		-- Test error methods exist
		if type(err.kind) ~= "function" then
			error("error should have kind method")
		end
		if type(err.message) ~= "function" then
			error("error should have message method")
		end
		if type(err.retryable) ~= "function" then
			error("error should have retryable method")
		end

		-- Test error values
		if err:retryable() ~= false then
			error("error should not be retryable")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestStoreMethodsExist(t *testing.T) {
	reg := newMockRegistry()
	mem := newMemoryStore()
	reg.Register("test:mystore", mem)

	l := setupStateWithContext(reg)
	defer l.Close()

	err := l.DoString(`
		local s = store.get("test:mystore")
		if s == nil then
			error("store.get should return store object")
		end

		-- Verify all methods exist
		if type(s.get) ~= "function" then error("s:get should be a method") end
		if type(s.set) ~= "function" then error("s:set should be a method") end
		if type(s.has) ~= "function" then error("s:has should be a method") end
		if type(s.delete) ~= "function" then error("s:delete should be a method") end
		if type(s.release) ~= "function" then error("s:release should be a method") end

		s:release()
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}
