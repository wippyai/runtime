// SPDX-License-Identifier: MPL-2.0

package system

import (
	lua "github.com/wippyai/go-lua"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/runtime/security"
	systemkv "github.com/wippyai/runtime/system/kv"
)

// createLockTable builds the system.lock surface. A lock is an entry in the
// shared kv at _sys:lock:<name> holding the owner PID: mutual exclusion is
// linearizable via raft, and a holder's locks auto-release when the holder
// process exits (topology monitor) or its node leaves the cluster.
func createLockTable() *lua.LTable {
	t := lua.CreateTable(0, 2)
	t.RawSetString("acquire", lua.LGoFunc(lockAcquire))
	t.RawSetString("release", lua.LGoFunc(lockRelease))
	t.Immutable = true
	return t
}

func lockAcquire(l *lua.LState) int {
	name := l.CheckString(1)

	if !security.IsAllowed(l.Context(), "system.lock", name, nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: system.lock on "+name).WithKind(lua.PermissionDenied).WithRetryable(false))
		return 2
	}

	ls := systemkv.GetLockService(l.Context())
	if ls == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "lock service not available").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	p, ok := runtimeapi.GetFramePID(l.Context())
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "caller PID not available").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	acquired, err := ls.Acquire(name, p)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "lock acquire").WithRetryable(false))
		return 2
	}
	if !acquired {
		l.Push(lua.LBool(false))
		l.Push(lua.NewLuaError(l, "lock held: "+name).WithKind(lua.AlreadyExists).WithRetryable(false))
		return 2
	}

	l.Push(lua.LBool(true))
	l.Push(lua.LNil)
	return 2
}

func lockRelease(l *lua.LState) int {
	name := l.CheckString(1)

	if !security.IsAllowed(l.Context(), "system.lock", name, nil) {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "permission denied: system.lock on "+name).WithKind(lua.PermissionDenied).WithRetryable(false))
		return 2
	}

	ls := systemkv.GetLockService(l.Context())
	if ls == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "lock service not available").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	p, ok := runtimeapi.GetFramePID(l.Context())
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "caller PID not available").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	// Release verifies the caller holds the lock; a non-holder gets false.
	released, err := ls.Release(name, p)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "lock release").WithRetryable(false))
		return 2
	}

	l.Push(lua.LBool(released))
	l.Push(lua.LNil)
	return 2
}
