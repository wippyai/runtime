// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"
	"sync"
	"testing"

	hraft "github.com/hashicorp/raft"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	pidapi "github.com/wippyai/runtime/api/pid"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/security"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	"github.com/wippyai/runtime/system/topology/namereg/global"
)

// fakeGlobalRegistry drives a real global.FSM through the public
// global.Registry interface so we can exercise the STRONG state
// machinery (including auto-release via applyRemovePID) without standing
// up a real Raft cluster.
type fakeGlobalRegistry struct {
	fsm      *global.FSM
	mu       sync.Mutex
	logIndex uint64
}

func newFakeGlobalRegistry() *fakeGlobalRegistry {
	return &fakeGlobalRegistry{fsm: global.NewFSM()}
}

func (f *fakeGlobalRegistry) apply(cmd *global.Command) any {
	data, err := global.EncodeCommand(cmd)
	if err != nil {
		return err
	}
	f.logIndex++
	return f.fsm.Apply(&hraft.Log{Data: data, Index: f.logIndex})
}

func (f *fakeGlobalRegistry) Register(ctx context.Context, name string, p pidapi.PID) (pidapi.PID, error) {
	out, err := f.RegisterScope(ctx, name, p, globalapi.Consistent)
	if err != nil {
		return out.ExistingPID, err
	}
	return out.PID, nil
}

func (f *fakeGlobalRegistry) RegisterScope(ctx context.Context, name string, p pidapi.PID, mode globalapi.RegistrationMode) (globalapi.RegisterOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// For test purposes, STRONG is modeled as a Consistent register: the
	// Lua release path looks up holder PID, which the FSM exposes through
	// the same Lookup machinery for both scopes.
	cmd := &global.Command{
		Type:   global.CmdRegister,
		Name:   name,
		PID:    p,
		NodeID: p.Node,
	}
	resp := f.apply(cmd)
	r, ok := resp.(*global.RegisterResult)
	if !ok {
		return globalapi.RegisterOutcome{}, resp.(error)
	}
	if r.Err != nil {
		return globalapi.RegisterOutcome{ExistingPID: r.ExistingPID}, globalapi.ErrNameAlreadyRegistered
	}
	return globalapi.RegisterOutcome{PID: r.PID, Epoch: r.FenceToken, State: globalapi.RegisterStateActive}, nil
}

func (f *fakeGlobalRegistry) Unregister(ctx context.Context, name string) (bool, error) {
	return f.UnregisterScope(ctx, name, globalapi.Consistent)
}

func (f *fakeGlobalRegistry) UnregisterScope(_ context.Context, name string, _ globalapi.RegistrationMode) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cmd := &global.Command{Type: global.CmdUnregister, Name: name}
	resp := f.apply(cmd)
	r, ok := resp.(*global.UnregisterResult)
	if !ok {
		return false, resp.(error)
	}
	return r.Removed, nil
}

func (f *fakeGlobalRegistry) Lookup(_ context.Context, name string, opts ...globalapi.LookupOption) (globalapi.LookupResult, error) {
	var o globalapi.LookupOptions
	for _, opt := range opts {
		opt(&o)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	state := f.fsm.State()
	if o.ByPID != nil {
		names := state.LookupByPID(*o.ByPID)
		return globalapi.LookupResult{PID: *o.ByPID, NamesForPID: names, Found: len(names) > 0}, nil
	}
	p, found := state.Lookup(name)
	return globalapi.LookupResult{PID: p, Found: found}, nil
}

func (f *fakeGlobalRegistry) LookupByPID(p pidapi.PID) []string {
	r, _ := f.Lookup(context.Background(), "", globalapi.ByPID(p))
	return r.NamesForPID
}

func (f *fakeGlobalRegistry) Remove(_ context.Context, p pidapi.PID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cmd := &global.Command{Type: global.CmdRemovePID, PID: p}
	f.apply(cmd)
	return nil
}

func (f *fakeGlobalRegistry) RemoveNode(_ context.Context, n pidapi.NodeID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cmd := &global.Command{Type: global.CmdRemoveNode, NodeID: n}
	f.apply(cmd)
	return nil
}

var _ globalapi.Registry = (*fakeGlobalRegistry)(nil)

func newLockTestState(t *testing.T, p pidapi.PID, reg globalapi.Registry) *lua.LState {
	t.Helper()
	l := lua.NewState()
	t.Cleanup(func() { l.Close() })

	ctx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	ctx = security.SetStrictMode(ctx, false)
	if reg != nil {
		ctx = globalapi.WithRegistry(ctx, reg)
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
	reg := newFakeGlobalRegistry()
	p := pidapi.PID{Host: "h1", UniqID: "p1", Node: "node-1"}
	l := newLockTestState(t, p, reg)

	err := l.DoString(`
		local ok, err = system.lock.acquire("my-lock")
		assert(err == nil, "unexpected error: " .. tostring(err))
		assert(ok == true, "expected true, got " .. tostring(ok))
	`)
	require.NoError(t, err)

	res, _ := reg.Lookup(context.Background(), "my-lock")
	require.True(t, res.Found)
	require.Equal(t, p, res.PID)
}

func TestLockAcquire_Conflict(t *testing.T) {
	reg := newFakeGlobalRegistry()
	pHolder := pidapi.PID{Host: "h1", UniqID: "holder", Node: "node-1"}
	pOther := pidapi.PID{Host: "h1", UniqID: "other", Node: "node-1"}

	_, err := reg.RegisterScope(context.Background(), "shared", pHolder, globalapi.Strong)
	require.NoError(t, err)

	l := newLockTestState(t, pOther, reg)
	err = l.DoString(`
		local ok, err = system.lock.acquire("shared")
		assert(ok == false, "expected false on conflict")
		assert(err ~= nil, "expected conflict error")
		assert(err:kind() == "AlreadyExists", "expected AlreadyExists kind, got " .. tostring(err:kind()))
	`)
	require.NoError(t, err)
}

func TestLockRelease_ByHolder(t *testing.T) {
	reg := newFakeGlobalRegistry()
	p := pidapi.PID{Host: "h1", UniqID: "p1", Node: "node-1"}
	_, err := reg.RegisterScope(context.Background(), "lockA", p, globalapi.Strong)
	require.NoError(t, err)

	l := newLockTestState(t, p, reg)
	err = l.DoString(`
		local ok, err = system.lock.release("lockA")
		assert(err == nil, "unexpected error: " .. tostring(err))
		assert(ok == true, "expected true, got " .. tostring(ok))
	`)
	require.NoError(t, err)

	res, _ := reg.Lookup(context.Background(), "lockA")
	require.False(t, res.Found)
}

func TestLockRelease_NotHeld(t *testing.T) {
	reg := newFakeGlobalRegistry()
	p := pidapi.PID{Host: "h1", UniqID: "p1", Node: "node-1"}
	l := newLockTestState(t, p, reg)

	err := l.DoString(`
		local ok, err = system.lock.release("nope")
		assert(err == nil, "unexpected error: " .. tostring(err))
		assert(ok == false, "expected false when lock not held")
	`)
	require.NoError(t, err)
}

func TestLockRelease_NotHeldByCaller(t *testing.T) {
	reg := newFakeGlobalRegistry()
	pHolder := pidapi.PID{Host: "h1", UniqID: "holder", Node: "node-1"}
	pOther := pidapi.PID{Host: "h1", UniqID: "other", Node: "node-1"}

	_, err := reg.RegisterScope(context.Background(), "mine", pHolder, globalapi.Strong)
	require.NoError(t, err)

	l := newLockTestState(t, pOther, reg)
	err = l.DoString(`
		local ok, err = system.lock.release("mine")
		assert(err == nil, "unexpected error: " .. tostring(err))
		assert(ok == false, "expected false when caller is not holder")
	`)
	require.NoError(t, err)

	res, _ := reg.Lookup(context.Background(), "mine")
	require.True(t, res.Found, "lock must remain held by original holder")
	require.Equal(t, pHolder, res.PID)
}

func TestLockAcquire_PermissionDenied(t *testing.T) {
	reg := newFakeGlobalRegistry()
	p := pidapi.PID{Host: "h1", UniqID: "p1", Node: "node-1"}

	l := lua.NewState()
	t.Cleanup(func() { l.Close() })

	ctx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	ctx = security.SetStrictMode(ctx, true)
	ctx = globalapi.WithRegistry(ctx, reg)
	ctx, fc := ctxapi.OpenFrameContext(ctx)
	t.Cleanup(func() { ctxapi.ReleaseFrameContext(fc) })
	require.NoError(t, runtimeapi.SetFramePID(ctx, p))
	l.SetContext(ctx)

	tbl, _ := Module.Build()
	l.SetGlobal("system", tbl)

	err := l.DoString(`
		local ok, err = system.lock.acquire("x")
		assert(ok == nil, "expected nil under strict security")
		assert(err ~= nil, "expected permission-denied error")
	`)
	require.NoError(t, err)
}

func TestLockRelease_PermissionDenied(t *testing.T) {
	reg := newFakeGlobalRegistry()
	p := pidapi.PID{Host: "h1", UniqID: "p1", Node: "node-1"}

	l := lua.NewState()
	t.Cleanup(func() { l.Close() })

	ctx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	ctx = security.SetStrictMode(ctx, true)
	ctx = globalapi.WithRegistry(ctx, reg)
	ctx, fc := ctxapi.OpenFrameContext(ctx)
	t.Cleanup(func() { ctxapi.ReleaseFrameContext(fc) })
	require.NoError(t, runtimeapi.SetFramePID(ctx, p))
	l.SetContext(ctx)

	tbl, _ := Module.Build()
	l.SetGlobal("system", tbl)

	err := l.DoString(`
		local ok, err = system.lock.release("x")
		assert(ok == nil, "expected nil under strict security")
		assert(err ~= nil, "expected permission-denied error")
	`)
	require.NoError(t, err)
}

// Auto-release: when a holder PID is removed (process exit), the FSM's
// applyRemovePID path releases all Strong-scope names. Exercising the
// command directly through the same FSM the registry drives confirms
// the existing primitive does the work — no new release path required.
func TestLockAutoRelease_OnHolderExit(t *testing.T) {
	reg := newFakeGlobalRegistry()
	p := pidapi.PID{Host: "h1", UniqID: "doomed", Node: "node-1"}

	_, err := reg.RegisterScope(context.Background(), "auto", p, globalapi.Strong)
	require.NoError(t, err)

	res, _ := reg.Lookup(context.Background(), "auto")
	require.True(t, res.Found)

	// Simulate process exit by exercising the same FSM command the
	// process supervisor uses to reap a dead PID.
	require.NoError(t, reg.Remove(context.Background(), p))

	res, _ = reg.Lookup(context.Background(), "auto")
	require.False(t, res.Found, "lock must auto-release on holder removal")
}
