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

func (s *memoryStore) StoreInfo(_ context.Context) store.Info {
	return store.Info{
		Backend:        store.BackendMemory,
		Consistency:    store.ConsistencyLocal,
		Durable:        false,
		List:           true,
		Versioned:      false,
		ConditionalPut: false,
		TTL:            true,
	}
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

func TestModuleConstants(t *testing.T) {
	l := setupState()
	defer l.Close()

	mod := l.GetGlobal("store").(*lua.LTable)
	backend := mod.RawGetString("backend").(*lua.LTable)
	consistency := mod.RawGetString("consistency").(*lua.LTable)

	if backend.RawGetString("KV_RAFT") != lua.LString("kv.raft") {
		t.Error("backend.KV_RAFT constant mismatch")
	}
	if backend.RawGetString("KV_CRDT") != lua.LString("kv.crdt") {
		t.Error("backend.KV_CRDT constant mismatch")
	}
	if backend.RawGetString("MEMORY") != lua.LString("memory") {
		t.Error("backend.MEMORY constant mismatch")
	}
	if backend.RawGetString("SQL") != lua.LString("sql") {
		t.Error("backend.SQL constant mismatch")
	}
	if consistency.RawGetString("LINEARIZABLE") != lua.LString("linearizable") {
		t.Error("consistency.LINEARIZABLE constant mismatch")
	}
	if consistency.RawGetString("EVENTUAL") != lua.LString("eventual") {
		t.Error("consistency.EVENTUAL constant mismatch")
	}
	if consistency.RawGetString("LOCAL") != lua.LString("local") {
		t.Error("consistency.LOCAL constant mismatch")
	}
	if !backend.Immutable || !consistency.Immutable {
		t.Error("constant tables should be immutable")
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

func TestStoreInfo(t *testing.T) {
	reg := newMockRegistry()
	mem := newMemoryStore()
	reg.Register("test:mystore", mem)

	l := setupStateWithContext(reg)
	defer l.Close()

	err := l.DoString(`
		local s, err = store.get("test:mystore")
		if err then error("unexpected get error: " .. tostring(err)) end

		local info, info_err = s:info()
		if info_err then error("unexpected info error: " .. tostring(info_err)) end
		if info.id ~= "test:mystore" then error("id mismatch: " .. tostring(info.id)) end
		if info.backend ~= store.backend.MEMORY then error("backend mismatch") end
		if info.consistency ~= store.consistency.LOCAL then error("consistency mismatch") end
		if info.durable ~= false then error("durable mismatch") end
		if info.list ~= true then error("list mismatch") end
		if info.versioned ~= false then error("versioned mismatch") end
		if info.conditional_put ~= false then error("conditional_put mismatch") end
		if info.ttl ~= true then error("ttl mismatch") end
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
		if type(s.info) ~= "function" then error("s:info should be a method") end
		if type(s.entry) ~= "function" then error("s:entry should be a method") end
		if type(s.list) ~= "function" then error("s:list should be a method") end
		if type(s.put) ~= "function" then error("s:put should be a method") end
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

func TestRichYieldHandleResults(t *testing.T) {
	l := setupState()
	defer l.Close()

	entry := store.VersionedEntry{
		Entry:   store.Entry{Key: registry.ParseID("test:key"), Value: payload.NewPayload(lua.LString("value"), payload.Lua)},
		Version: 42,
	}

	entryVals := (&EntryYield{}).HandleResult(l, store.EntryResponse{Entry: entry}, nil)
	if len(entryVals) != 2 || entryVals[1] != lua.LNil {
		t.Fatalf("entry yield returned unexpected values: %#v", entryVals)
	}
	entryTable, ok := entryVals[0].(*lua.LTable)
	if !ok {
		t.Fatalf("entry yield value = %T, want table", entryVals[0])
	}
	if entryTable.RawGetString("key") != lua.LString("test:key") {
		t.Error("entry key mismatch")
	}
	if entryTable.RawGetString("value") != lua.LString("value") {
		t.Error("entry value mismatch")
	}
	if entryTable.RawGetString("version") != lua.LString("42") {
		t.Error("entry version mismatch")
	}

	pageVals := (&ListYield{}).HandleResult(l, store.ListResponse{Page: store.Page{
		Items:   []store.VersionedEntry{entry},
		Cursor:  "test:key",
		HasMore: true,
	}}, nil)
	if len(pageVals) != 2 || pageVals[1] != lua.LNil {
		t.Fatalf("list yield returned unexpected values: %#v", pageVals)
	}
	pageTable := pageVals[0].(*lua.LTable)
	items := pageTable.RawGetString("items").(*lua.LTable)
	if items.Len() != 1 {
		t.Fatalf("items length = %d, want 1", items.Len())
	}
	if pageTable.RawGetString("cursor") != lua.LString("test:key") {
		t.Error("cursor mismatch")
	}
	if pageTable.RawGetString("has_more") != lua.LTrue {
		t.Error("has_more mismatch")
	}

	putVals := (&PutYield{}).HandleResult(l, store.PutResponse{Entry: entry}, nil)
	if len(putVals) != 2 || putVals[1] != lua.LNil {
		t.Fatalf("put yield returned unexpected values: %#v", putVals)
	}
}

func TestParseRichOptions(t *testing.T) {
	l := setupState()
	defer l.Close()

	t.Run("list limit", func(t *testing.T) {
		tbl := l.NewTable()
		tbl.RawSetString("limit", lua.LInteger(50))
		l.Push(tbl)
		opts, msg := parseListOptions(l, 1)
		l.Pop(1)
		if msg != "" || opts.Limit != 50 {
			t.Fatalf("parseListOptions = limit %d msg %q, want 50 and no error", opts.Limit, msg)
		}

		tbl = l.NewTable()
		tbl.RawSetString("limit", lua.LNumber(2.5))
		l.Push(tbl)
		_, msg = parseListOptions(l, 1)
		l.Pop(1)
		if msg == "" {
			t.Fatal("fractional list limit should fail")
		}
	})

	t.Run("if_version", func(t *testing.T) {
		parse := func(v lua.LValue) (store.PutOptions, string) {
			tbl := l.NewTable()
			tbl.RawSetString("if_version", v)
			l.Push(tbl)
			opts, msg := parsePutOptions(l, 1)
			l.Pop(1)
			return opts, msg
		}

		opts, msg := parse(lua.LString("18446744073709551615"))
		if msg != "" || !opts.HasVersion || opts.Version != store.Version(^uint64(0)) {
			t.Fatalf("string if_version = %#v msg %q, want max uint64", opts, msg)
		}

		opts, msg = parse(lua.LInteger(42))
		if msg != "" || !opts.HasVersion || opts.Version != 42 {
			t.Fatalf("integer if_version = %#v msg %q, want 42", opts, msg)
		}

		if _, msg = parse(lua.LNumber(9007199254740993)); msg == "" {
			t.Fatal("unsafe numeric if_version should fail")
		}
		if _, msg = parse(lua.LString("0")); msg == "" {
			t.Fatal("zero if_version should fail")
		}
	})
}

func TestRichYieldErrorKinds(t *testing.T) {
	l := setupState()
	defer l.Close()

	tests := []struct {
		name  string
		resp  any
		yield interface {
			HandleResult(*lua.LState, any, error) []lua.LValue
		}
		kind lua.Kind
	}{
		{
			name:  "entry not found",
			resp:  store.EntryResponse{Error: store.ErrKeyNotFound},
			yield: &EntryYield{},
			kind:  lua.NotFound,
		},
		{
			name:  "put already exists",
			resp:  store.PutResponse{Error: store.ErrKeyExists},
			yield: &PutYield{},
			kind:  lua.AlreadyExists,
		},
		{
			name:  "put conflict",
			resp:  store.PutResponse{Error: store.ErrVersionMismatch},
			yield: &PutYield{},
			kind:  lua.Conflict,
		},
		{
			name:  "list unsupported",
			resp:  store.ListResponse{Error: store.ErrUnsupported},
			yield: &ListYield{},
			kind:  lua.Invalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vals := tt.yield.HandleResult(l, tt.resp, nil)
			if vals[0] != lua.LNil {
				t.Fatalf("first value = %v, want nil", vals[0])
			}
			luaErr, ok := vals[1].(*lua.Error)
			if !ok {
				t.Fatalf("second value = %T, want *lua.Error", vals[1])
			}
			if luaErr.Kind() != tt.kind {
				t.Fatalf("kind = %s, want %s", luaErr.Kind(), tt.kind)
			}
		})
	}
}
