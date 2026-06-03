// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	pidapi "github.com/wippyai/runtime/api/pid"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/system/eventbus"
	systemkv "github.com/wippyai/runtime/system/kv"
	"go.uber.org/zap"
)

// newTestLocks builds a lock service over an in-memory kv engine (no topology
// monitor; auto-release-on-node-leave is proven separately in clustertest).
func newTestLocks(t *testing.T) *systemkv.LockService {
	t.Helper()
	eng := systemkv.NewService("lock", eventbus.NewBus(), zap.NewNop())
	if _, err := eng.Start(context.Background()); err != nil {
		t.Fatalf("engine start: %v", err)
	}
	t.Cleanup(func() { _ = eng.Stop(context.Background()) })
	return systemkv.NewLockService(eng, nil, "node-1", zap.NewNop())
}

func newLockTestState(t *testing.T, p pidapi.PID, ls *systemkv.LockService, strict bool) *lua.LState {
	t.Helper()
	l := lua.NewState()
	t.Cleanup(func() { l.Close() })

	ctx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	ctx = security.SetStrictMode(ctx, strict)
	if ls != nil {
		ctx = systemkv.WithLockService(ctx, ls)
	}

	ctx, fc := ctxapi.OpenFrameContext(ctx)
	t.Cleanup(func() { ctxapi.ReleaseFrameContext(fc) })
	require.NoError(t, runtimeapi.SetFramePID(ctx, p))
	l.SetContext(ctx)

	tbl, _ := Module.Build()
	l.SetGlobal("system", tbl)
	return l
}

func TestLockAcquire_Success(t *testing.T) {
	ls := newTestLocks(t)
	p := pidapi.PID{Host: "h1", UniqID: "p1", Node: "node-1"}
	l := newLockTestState(t, p, ls, false)

	require.NoError(t, l.DoString(`
		local ok, err = system.lock.acquire("my-lock")
		assert(err == nil, "unexpected error: " .. tostring(err))
		assert(ok == true, "expected true, got " .. tostring(ok))
	`))

	h, ok, _ := ls.Holder("my-lock")
	require.True(t, ok)
	require.Equal(t, p.String(), h.String())
}

func TestLockAcquire_Conflict(t *testing.T) {
	ls := newTestLocks(t)
	pHolder := pidapi.PID{Host: "h1", UniqID: "holder", Node: "node-1"}
	pOther := pidapi.PID{Host: "h1", UniqID: "other", Node: "node-1"}
	mustHold(t, ls, "shared", pHolder)

	l := newLockTestState(t, pOther, ls, false)
	require.NoError(t, l.DoString(`
		local ok, err = system.lock.acquire("shared")
		assert(ok == false, "expected false on conflict")
		assert(err ~= nil, "expected conflict error")
		assert(err:kind() == "AlreadyExists", "expected AlreadyExists, got " .. tostring(err:kind()))
	`))
}

func TestLockRelease_ByHolder(t *testing.T) {
	ls := newTestLocks(t)
	p := pidapi.PID{Host: "h1", UniqID: "p1", Node: "node-1"}
	mustHold(t, ls, "lockA", p)

	l := newLockTestState(t, p, ls, false)
	require.NoError(t, l.DoString(`
		local ok, err = system.lock.release("lockA")
		assert(err == nil, "unexpected error: " .. tostring(err))
		assert(ok == true, "expected true, got " .. tostring(ok))
	`))
	if _, ok, _ := ls.Holder("lockA"); ok {
		t.Fatalf("lockA must be released")
	}
}

func TestLockRelease_NotHeld(t *testing.T) {
	ls := newTestLocks(t)
	p := pidapi.PID{Host: "h1", UniqID: "p1", Node: "node-1"}
	l := newLockTestState(t, p, ls, false)
	require.NoError(t, l.DoString(`
		local ok, err = system.lock.release("nope")
		assert(err == nil, "unexpected error: " .. tostring(err))
		assert(ok == false, "expected false when lock not held")
	`))
}

func TestLockRelease_NotHeldByCaller(t *testing.T) {
	ls := newTestLocks(t)
	pHolder := pidapi.PID{Host: "h1", UniqID: "holder", Node: "node-1"}
	pOther := pidapi.PID{Host: "h1", UniqID: "other", Node: "node-1"}
	mustHold(t, ls, "mine", pHolder)

	l := newLockTestState(t, pOther, ls, false)
	require.NoError(t, l.DoString(`
		local ok, err = system.lock.release("mine")
		assert(err == nil, "unexpected error: " .. tostring(err))
		assert(ok == false, "expected false when caller is not holder")
	`))
	h, ok, _ := ls.Holder("mine")
	require.True(t, ok, "lock must remain held")
	require.Equal(t, pHolder.String(), h.String())
}

func TestLockAcquire_PermissionDenied(t *testing.T) {
	ls := newTestLocks(t)
	p := pidapi.PID{Host: "h1", UniqID: "p1", Node: "node-1"}
	l := newLockTestState(t, p, ls, true)
	require.NoError(t, l.DoString(`
		local ok, err = system.lock.acquire("x")
		assert(ok == nil, "expected nil under strict security")
		assert(err ~= nil, "expected permission-denied error")
	`))
}

func TestLockRelease_PermissionDenied(t *testing.T) {
	ls := newTestLocks(t)
	p := pidapi.PID{Host: "h1", UniqID: "p1", Node: "node-1"}
	l := newLockTestState(t, p, ls, true)
	require.NoError(t, l.DoString(`
		local ok, err = system.lock.release("x")
		assert(ok == nil, "expected nil under strict security")
		assert(err ~= nil, "expected permission-denied error")
	`))
}

// TestLockAutoRelease_OnHolderExit proves a holder's lock is released when the
// holder process exits (ReapPID, driven by the topology monitor in production).
func TestLockAutoRelease_OnHolderExit(t *testing.T) {
	ls := newTestLocks(t)
	p := pidapi.PID{Host: "h1", UniqID: "doomed", Node: "node-1"}
	mustHold(t, ls, "auto", p)

	ls.ReapPID(p)

	if _, ok, _ := ls.Holder("auto"); ok {
		t.Fatalf("lock must auto-release on holder exit")
	}
}

func mustHold(t *testing.T, ls *systemkv.LockService, name string, holder pidapi.PID) {
	t.Helper()
	ok, err := ls.Acquire(name, holder)
	require.NoError(t, err)
	require.True(t, ok)
}
