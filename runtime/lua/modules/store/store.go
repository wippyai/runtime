// SPDX-License-Identifier: MPL-2.0

package store

import (
	"context"
	"fmt"
	"math"
	"strconv"
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
	"info":    storeInfo,
	"get":     storeKeyGet,
	"entry":   storeKeyEntry,
	"list":    storeKeyList,
	"put":     storeKeyPut,
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

func permissionDeniedError(l *lua.LState, msg string) int {
	err := lua.NewLuaError(l, msg).
		WithKind(lua.PermissionDenied).
		WithRetryable(false)
	l.Push(lua.LNil)
	l.Push(err)
	return 2
}

func activeStore(l *lua.LState) (*Store, store.Store, bool) {
	s := checkStore(l)
	if s == nil {
		return nil, nil, false
	}

	s.mu.Lock()
	if s.released {
		s.mu.Unlock()
		invalidError(l, "store is released")
		return nil, nil, false
	}
	storeImpl := s.store
	s.mu.Unlock()
	return s, storeImpl, true
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

func storeInfo(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		return invalidError(l, "no context")
	}

	s, storeImpl, ok := activeStore(l)
	if !ok {
		return 2
	}
	if !security.IsAllowed(ctx, "store.info", s.id, nil) {
		return permissionDeniedError(l, fmt.Sprintf("not allowed to inspect store: %s", s.id))
	}

	info := store.Inspect(ctx, registry.ParseID(s.id), storeImpl)
	l.Push(pushInfoTable(l, info))
	l.Push(lua.LNil)
	return 2
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

func storeKeyEntry(l *lua.LState) int {
	s, storeImpl, ok := activeStore(l)
	if !ok {
		return 2
	}

	keyStr := l.CheckString(2)
	if keyStr == "" {
		return invalidError(l, "key is required")
	}

	if !security.IsAllowed(l.Context(), "store.key.get", s.id, map[string]any{"key": keyStr}) {
		return permissionDeniedError(l, fmt.Sprintf("not allowed to read key: %s", keyStr))
	}

	yield := AcquireEntryYield()
	yield.Store = storeImpl
	yield.Key = registry.ParseID(keyStr)
	l.Push(yield)
	return -1
}

func storeKeyList(l *lua.LState) int {
	s, storeImpl, ok := activeStore(l)
	if !ok {
		return 2
	}

	opts, errMsg := parseListOptions(l, 2)
	if errMsg != "" {
		return invalidError(l, errMsg)
	}

	if !security.IsAllowed(l.Context(), "store.key.list", s.id, map[string]any{"prefix": opts.Prefix}) {
		return permissionDeniedError(l, fmt.Sprintf("not allowed to list store: %s", s.id))
	}

	yield := AcquireListYield()
	yield.Store = storeImpl
	yield.Opts = opts
	l.Push(yield)
	return -1
}

func storeKeyPut(l *lua.LState) int {
	s, storeImpl, ok := activeStore(l)
	if !ok {
		return 2
	}

	keyStr := l.CheckString(2)
	if keyStr == "" {
		return invalidError(l, "key is required")
	}

	if !security.IsAllowed(l.Context(), "store.key.set", s.id, map[string]any{"key": keyStr}) {
		return permissionDeniedError(l, fmt.Sprintf("not allowed to write key: %s", keyStr))
	}

	luaVal := l.Get(3)
	if luaVal == lua.LNil {
		return invalidError(l, "value is required")
	}

	opts, errMsg := parsePutOptions(l, 4)
	if errMsg != "" {
		return invalidError(l, errMsg)
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

	yield := AcquirePutYield()
	yield.Store = storeImpl
	yield.Key = registry.ParseID(keyStr)
	yield.Value = goPayload
	yield.Opts = opts
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

func parseListOptions(l *lua.LState, idx int) (store.ListOptions, string) {
	if l.GetTop() < idx {
		return store.ListOptions{}, ""
	}
	v := l.Get(idx)
	if v == lua.LNil {
		return store.ListOptions{}, ""
	}
	tbl, ok := v.(*lua.LTable)
	if !ok {
		return store.ListOptions{}, "options must be a table"
	}

	var opts store.ListOptions
	if prefix := tbl.RawGetString("prefix"); prefix != lua.LNil {
		s, ok := prefix.(lua.LString)
		if !ok {
			return opts, "prefix must be a string"
		}
		opts.Prefix = string(s)
	}
	if after := tbl.RawGetString("after"); after != lua.LNil {
		s, ok := after.(lua.LString)
		if !ok {
			return opts, "after must be a string"
		}
		opts.After = string(s)
	}
	if limit := tbl.RawGetString("limit"); limit != lua.LNil {
		n, ok := nonNegativeIntFromLua(limit)
		if !ok {
			return opts, "limit must be a non-negative integer"
		}
		opts.Limit = n
	}
	return opts, ""
}

func parsePutOptions(l *lua.LState, idx int) (store.PutOptions, string) {
	if l.GetTop() < idx {
		return store.PutOptions{}, ""
	}
	v := l.Get(idx)
	if v == lua.LNil {
		return store.PutOptions{}, ""
	}
	tbl, ok := v.(*lua.LTable)
	if !ok {
		return store.PutOptions{}, "options must be a table"
	}

	var opts store.PutOptions
	if ttl := tbl.RawGetString("ttl"); ttl != lua.LNil {
		n, ok := numberFromLua(ttl)
		if !ok || n < 0 {
			return opts, "ttl must be a non-negative number"
		}
		opts.TTL = time.Duration(n * float64(time.Second))
	}
	if onlyIfAbsent := tbl.RawGetString("only_if_absent"); onlyIfAbsent != lua.LNil {
		b, ok := onlyIfAbsent.(lua.LBool)
		if !ok {
			return opts, "only_if_absent must be a boolean"
		}
		opts.OnlyIfAbsent = bool(b)
	}
	if ifVersion := tbl.RawGetString("if_version"); ifVersion != lua.LNil {
		version, ok := versionFromLua(ifVersion)
		if !ok || version == 0 {
			return opts, "if_version must be a positive version"
		}
		opts.HasVersion = true
		opts.Version = version
	}
	if opts.OnlyIfAbsent && opts.HasVersion {
		return opts, "only_if_absent and if_version are mutually exclusive"
	}
	return opts, ""
}

func numberFromLua(v lua.LValue) (float64, bool) {
	switch n := v.(type) {
	case lua.LNumber:
		return float64(n), true
	case lua.LInteger:
		return float64(n), true
	default:
		return 0, false
	}
}

func nonNegativeIntFromLua(v lua.LValue) (int, bool) {
	maxInt := int(^uint(0) >> 1)
	switch n := v.(type) {
	case lua.LInteger:
		if n < 0 || uint64(n) > uint64(maxInt) {
			return 0, false
		}
		return int(n), true
	case lua.LNumber:
		f := float64(n)
		if f < 0 || math.Trunc(f) != f || f > float64(maxInt) {
			return 0, false
		}
		return int(f), true
	default:
		return 0, false
	}
}

func versionFromLua(v lua.LValue) (store.Version, bool) {
	switch n := v.(type) {
	case lua.LString:
		parsed, err := strconv.ParseUint(string(n), 10, 64)
		if err != nil {
			return 0, false
		}
		return store.Version(parsed), true
	case lua.LInteger:
		if n < 0 {
			return 0, false
		}
		return store.Version(uint64(n)), true
	case lua.LNumber:
		f := float64(n)
		const maxExactLuaInteger = float64(1<<53 - 1)
		if f < 0 || math.Trunc(f) != f || f > maxExactLuaInteger {
			return 0, false
		}
		return store.Version(uint64(f)), true
	default:
		return 0, false
	}
}

func pushInfoTable(l *lua.LState, info store.Info) lua.LValue {
	t := l.NewTable()
	t.RawSetString("id", lua.LString(info.ID.String()))
	t.RawSetString("backend", lua.LString(info.Backend))
	t.RawSetString("consistency", lua.LString(info.Consistency))
	t.RawSetString("durable", lua.LBool(info.Durable))
	t.RawSetString("list", lua.LBool(info.List))
	t.RawSetString("versioned", lua.LBool(info.Versioned))
	t.RawSetString("conditional_put", lua.LBool(info.ConditionalPut))
	t.RawSetString("ttl", lua.LBool(info.TTL))
	return t
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
