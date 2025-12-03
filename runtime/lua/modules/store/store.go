package store

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	rtresource "github.com/wippyai/runtime/api/runtime/resource"
	"github.com/wippyai/runtime/api/store"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/lua/security"
	lua "github.com/yuin/gopher-lua"
)

type Store struct {
	resource      resource.Resource[any]
	store         store.Store
	released      bool
	mu            sync.Mutex
	cancelCleanup func()
}

func NewStore(ctx context.Context, res resource.Resource[any], s store.Store) *Store {
	storeWrapper := &Store{
		resource: res,
		store:    s,
		released: false,
	}

	resStore := rtresource.GetStore(ctx)
	if resStore != nil {
		storeWrapper.cancelCleanup = resStore.AddCleanup(func() error {
			storeWrapper.mu.Lock()
			defer storeWrapper.mu.Unlock()
			if !storeWrapper.released && storeWrapper.resource != nil {
				storeWrapper.resource.Release()
				storeWrapper.released = true
			}
			return nil
		})
	}

	return storeWrapper
}

func (s *Store) GetStore() store.Store {
	return s.store
}

var storeMethods = map[string]lua.LGFunction{
	"get":     storeKeyGet,
	"set":     storeKeySet,
	"delete":  storeKeyDelete,
	"has":     storeKeyHas,
	"release": storeRelease,
}

func checkStore(l *lua.LState) *Store {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*Store); ok {
		return v
	}
	l.ArgError(1, "store expected")
	return nil
}

func storeGet(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context"))
		return 2
	}

	id := l.CheckString(1)
	if id == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("resource id is required"))
		return 2
	}

	if !security.IsAllowed(ctx, "store.get", id, nil) {
		l.RaiseError("not allowed to access store: %s", id)
		return 0
	}

	reg := resource.GetRegistry(ctx)
	if reg == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("resource registry not found"))
		return 2
	}

	resID := registry.ParseID(id)
	res, err := reg.Acquire(ctx, resID, resource.ModeNormal)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to acquire resource: %v", err)))
		return 2
	}

	storeRes, err := res.Get()
	if err != nil {
		res.Release()
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to get resource: %v", err)))
		return 2
	}

	storeImpl, ok := storeRes.(store.Store)
	if !ok {
		res.Release()
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("resource is not a store: %T", storeRes)))
		return 2
	}

	s := NewStore(ctx, res, storeImpl)

	value.NewUserData(l, s, storeMetatable)
	return 1
}

func storeKeyGet(l *lua.LState) int {
	s := checkStore(l)
	if s == nil {
		return 0
	}

	s.mu.Lock()
	if s.released {
		s.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.LString("store is released"))
		return 2
	}
	storeImpl := s.store
	s.mu.Unlock()

	keyStr := l.CheckString(2)
	if keyStr == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("key is required"))
		return 2
	}

	if !security.IsAllowed(l.Context(), "store.key.get", keyStr, nil) {
		l.RaiseError("not allowed to read key: %s", keyStr)
		return 0
	}

	parsedKey := registry.ParseID(keyStr)
	yield := AcquireGetYield()
	yield.Store = storeImpl
	yield.Key = parsedKey
	l.Push(yield)
	return -1
}

func storeKeySet(l *lua.LState) int {
	s := checkStore(l)
	if s == nil {
		return 0
	}

	s.mu.Lock()
	if s.released {
		s.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.LString("store is released"))
		return 2
	}
	storeImpl := s.store
	s.mu.Unlock()

	keyStr := l.CheckString(2)
	if keyStr == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("key is required"))
		return 2
	}

	if !security.IsAllowed(l.Context(), "store.key.set", keyStr, nil) {
		l.RaiseError("not allowed to write key: %s", keyStr)
		return 0
	}

	luaVal := l.Get(3)
	if luaVal == lua.LNil {
		l.Push(lua.LNil)
		l.Push(lua.LString("value is required"))
		return 2
	}

	var ttl time.Duration
	if l.GetTop() >= 4 {
		ttlSeconds := l.CheckNumber(4)
		ttl = time.Duration(float64(ttlSeconds) * float64(time.Second))
	}

	transcoder := payload.GetTranscoder(l.Context())
	if transcoder == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("transcoder not found in context"))
		return 2
	}

	luaPayload := payload.NewPayload(luaVal, payload.Lua)
	goPayload, err := transcoder.Transcode(luaPayload, payload.Golang)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to transcode: %v", err)))
		return 2
	}

	parsedKey := registry.ParseID(keyStr)
	entry := store.Entry{
		Key:   parsedKey,
		Value: goPayload,
		TTL:   ttl,
	}
	yield := AcquireSetYield()
	yield.Store = storeImpl
	yield.Entry = entry
	l.Push(yield)
	return -1
}

func storeKeyDelete(l *lua.LState) int {
	s := checkStore(l)
	if s == nil {
		return 0
	}

	s.mu.Lock()
	if s.released {
		s.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.LString("store is released"))
		return 2
	}
	storeImpl := s.store
	s.mu.Unlock()

	keyStr := l.CheckString(2)
	if keyStr == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("key is required"))
		return 2
	}

	if !security.IsAllowed(l.Context(), "store.key.delete", keyStr, nil) {
		l.RaiseError("not allowed to delete key: %s", keyStr)
		return 0
	}

	parsedKey := registry.ParseID(keyStr)
	yield := AcquireDeleteYield()
	yield.Store = storeImpl
	yield.Key = parsedKey
	l.Push(yield)
	return -1
}

func storeKeyHas(l *lua.LState) int {
	s := checkStore(l)
	if s == nil {
		return 0
	}

	s.mu.Lock()
	if s.released {
		s.mu.Unlock()
		l.Push(lua.LNil)
		l.Push(lua.LString("store is released"))
		return 2
	}
	storeImpl := s.store
	s.mu.Unlock()

	keyStr := l.CheckString(2)
	if keyStr == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("key is required"))
		return 2
	}

	if !security.IsAllowed(l.Context(), "store.key.has", keyStr, nil) {
		l.RaiseError("not allowed to check key existence: %s", keyStr)
		return 0
	}

	parsedKey := registry.ParseID(keyStr)
	yield := AcquireHasYield()
	yield.Store = storeImpl
	yield.Key = parsedKey
	l.Push(yield)
	return -1
}

func storeRelease(l *lua.LState) int {
	s := checkStore(l)
	if s == nil {
		return 0
	}

	s.mu.Lock()
	if !s.released && s.resource != nil {
		s.resource.Release()
		s.resource = nil
		s.released = true
		cancel := s.cancelCleanup
		s.cancelCleanup = nil
		s.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	} else {
		s.mu.Unlock()
	}

	l.Push(lua.LTrue)
	return 1
}

func storeToString(l *lua.LState) int {
	s := checkStore(l)
	if s == nil {
		return 0
	}
	s.mu.Lock()
	released := s.released
	s.mu.Unlock()

	if released {
		l.Push(lua.LString("store.Store{released}"))
	} else {
		l.Push(lua.LString("store.Store{}"))
	}
	return 1
}
