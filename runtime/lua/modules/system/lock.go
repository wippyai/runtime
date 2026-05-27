// SPDX-License-Identifier: MPL-2.0

package system

import (
	"errors"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/globalreg"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/runtime/security"
)

// createLockTable builds the system.lock surface. Locks are STRONG-scope
// names with a holder PID. Authority and auto-release on holder death
// come from the existing globalreg STRONG machinery; this module adds
// no new FSM commands, gossip, or admin overrides.
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

	reg := globalreg.GetRegistry(l.Context())
	if reg == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "global registry not available").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	p, ok := runtimeapi.GetFramePID(l.Context())
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "caller PID not available").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	_, err := reg.RegisterScope(l.Context(), name, p, globalreg.Strong)
	if err != nil {
		if isConflict(err) {
			l.Push(lua.LBool(false))
			l.Push(lua.WrapErrorWithLua(l, err, "lock acquire").WithKind(lua.AlreadyExists).WithRetryable(false))
			return 2
		}
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "lock acquire").WithRetryable(false))
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

	reg := globalreg.GetRegistry(l.Context())
	if reg == nil {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "global registry not available").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	p, ok := runtimeapi.GetFramePID(l.Context())
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.NewLuaError(l, "caller PID not available").WithKind(lua.Internal).WithRetryable(false))
		return 2
	}

	// Verify caller holds the lock before unregistering. The globalreg
	// Strong unregister path itself does not enforce holder identity,
	// so authority lives here: a non-holder cannot drop someone else's
	// lock, and releasing a free name reports false rather than removed.
	res, err := reg.Lookup(l.Context(), name)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "lock release lookup").WithRetryable(false))
		return 2
	}
	if !res.Found || res.PID != p {
		l.Push(lua.LBool(false))
		l.Push(lua.LNil)
		return 2
	}

	removed, err := reg.UnregisterScope(l.Context(), name, globalreg.Strong)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.WrapErrorWithLua(l, err, "lock release").WithRetryable(false))
		return 2
	}

	l.Push(lua.LBool(removed))
	l.Push(lua.LNil)
	return 2
}

// isConflict reports whether a RegisterScope error is a name-taken
// conflict (already-active, pending-for-other-PID, or rejected by a
// peer during the Strong ack barrier). All three surface as
// apierror.AlreadyExists in globalreg.
func isConflict(err error) bool {
	if errors.Is(err, globalreg.ErrNameAlreadyRegistered) {
		return true
	}
	if errors.Is(err, globalreg.ErrPendingConflict) {
		return true
	}
	if errors.Is(err, globalreg.ErrStrongRegistrationRejected) {
		return true
	}
	return false
}
