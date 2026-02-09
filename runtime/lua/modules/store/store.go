package store

import (
	"context"
	"fmt"
	"sync"
	"time"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	rtresource "github.com/wippyai/runtime/api/runtime/resource"
	"github.com/wippyai/runtime/api/store"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/runtime/security"
)

type Store struct {
	resource      resource.Resource[any]
	store         store.Store
	cancelCleanup func()
	id            string
	mu            sync.Mutex
	released      bool
}

func NewStore(ctx context.Context, id string, res resource.Resource[any], s store.Store) *Store {
	storeWrapper := &Store{
		id:       id,
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

var storeMethods = map[string]lua.LGoFunc{
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

func invalidError(l *lua.LState, msg string) int {
	err := lua.NewLuaError(l, msg).
		WithKind(lua.Invalid).
		WithRetryable(false)
	l.Push(lua.LNil)
	l.Push(err)
	return 2
}

func notFoundError(l *lua.LState, msg string) int {
	err := lua.NewLuaError(l, msg).
		WithKind(lua.NotFound).
		WithRetryable(false)
	l.Push(lua.LNil)
	l.Push(err)
	return 2
}

func internalError(l *lua.LState, goErr error, context string) int {
	err := lua.WrapErrorWithLua(l, goErr, context).
		WithKind(lua.Internal).
		WithRetryable(false)
	l.Push(lua.LNil)
	l.Push(err)
	return 2
}

func storeGet(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		return invalidError(l, "no context")
	}

	id := l.CheckString(1)
	if id == "" {
		return invalidError(l, "resource id is required")
	}

	if !security.IsAllowed(ctx, "store.get", id, nil) {
		l.RaiseError("not allowed to access store: %s", id)
		return 0
	}

	reg := resource.GetRegistry(ctx)
	if reg == nil {
		return notFoundError(l, "resource registry not found")
	}

	resID := registry.ParseID(id)
	res, err := reg.Acquire(ctx, resID, resource.ModeNormal)
	if err != nil {
		return internalError(l, err, "failed to acquire resource")
	}

	storeRes, err := res.Get()
	if err != nil {
		res.Release()
		return internalError(l, err, "failed to get resource")
	}

	storeImpl, ok := storeRes.(store.Store)
	if !ok {
		res.Release()
		return invalidError(l, fmt.Sprintf("resource is not a store: %T", storeRes))
	}

	s := NewStore(ctx, id, res, storeImpl)

	value.PushTypedUserData(l, s, storeTypeName)
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
		return invalidError(l, "store is released")
	}
	storeImpl := s.store
	s.mu.Unlock()

	keyStr := l.CheckString(2)
	if keyStr == "" {
		return invalidError(l, "key is required")
	}

	if !security.IsAllowed(l.Context(), "store.key.get", s.id, map[string]any{"key": keyStr}) {
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
		return invalidError(l, "store is released")
	}
	storeImpl := s.store
	s.mu.Unlock()

	keyStr := l.CheckString(2)
	if keyStr == "" {
		return invalidError(l, "key is required")
	}

	if !security.IsAllowed(l.Context(), "store.key.set", s.id, map[string]any{"key": keyStr}) {
		l.RaiseError("not allowed to write key: %s", keyStr)
		return 0
	}

	luaVal := l.Get(3)
	if luaVal == lua.LNil {
		return invalidError(l, "value is required")
	}

	var ttl time.Duration
	if l.GetTop() >= 4 {
		ttlSeconds := l.CheckNumber(4)
		ttl = time.Duration(float64(ttlSeconds) * float64(time.Second))
	}

	transcoder := payload.GetTranscoder(l.Context())
	if transcoder == nil {
		return notFoundError(l, "transcoder not found in context")
	}

	luaPayload := payload.NewPayload(luaVal, payload.Lua)
	goPayload, err := transcoder.Transcode(luaPayload, payload.Golang)
	if err != nil {
		return internalError(l, err, "failed to transcode")
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
		return invalidError(l, "store is released")
	}
	storeImpl := s.store
	s.mu.Unlock()

	keyStr := l.CheckString(2)
	if keyStr == "" {
		return invalidError(l, "key is required")
	}

	if !security.IsAllowed(l.Context(), "store.key.delete", s.id, map[string]any{"key": keyStr}) {
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
		return invalidError(l, "store is released")
	}
	storeImpl := s.store
	s.mu.Unlock()

	keyStr := l.CheckString(2)
	if keyStr == "" {
		return invalidError(l, "key is required")
	}

	if !security.IsAllowed(l.Context(), "store.key.has", s.id, map[string]any{"key": keyStr}) {
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
