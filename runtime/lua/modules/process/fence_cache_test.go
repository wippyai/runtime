// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	globalregapi "github.com/wippyai/runtime/api/globalreg"
	pidapi "github.com/wippyai/runtime/api/pid"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/api/topology"
)

func TestFenceCache_GetCreatesCacheOnFirstAccess(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	defer CleanupFenceCache(l)

	cache := getFenceCache(l)
	assert.NotNil(t, cache)
	assert.Empty(t, cache)

	// Insert and verify
	cache["pid1"] = fenceEntry{globalName: "svc", fenceToken: 10}
	cache2 := getFenceCache(l)
	assert.Len(t, cache2, 1)
	assert.Equal(t, uint64(10), cache2["pid1"].fenceToken)
}

func TestFenceCache_SameLStateReturnsSameCache(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	defer CleanupFenceCache(l)

	c1 := getFenceCache(l)
	c1["x"] = fenceEntry{globalName: "a", fenceToken: 1}
	c2 := getFenceCache(l)
	assert.Equal(t, c1["x"], c2["x"])
}

func TestFenceCache_DifferentLStatesGetSeparateCaches(t *testing.T) {
	l1 := lua.NewState()
	l2 := lua.NewState()
	defer l1.Close()
	defer l2.Close()
	defer CleanupFenceCache(l1)
	defer CleanupFenceCache(l2)

	c1 := getFenceCache(l1)
	c1["shared"] = fenceEntry{globalName: "svc", fenceToken: 5}

	c2 := getFenceCache(l2)
	_, exists := c2["shared"]
	assert.False(t, exists, "different LStates should have separate caches")
}

func TestFenceCache_CleanupRemovesEntry(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	c := getFenceCache(l)
	c["pid"] = fenceEntry{globalName: "svc", fenceToken: 7}

	CleanupFenceCache(l)

	// After cleanup, a new getFenceCache should return empty map
	c2 := getFenceCache(l)
	assert.Empty(t, c2)
	defer CleanupFenceCache(l)
}

// ---------------------------------------------------------------------------
// fakeGlobalReg implements globalregapi.Registry for resolvePID tests.
// ---------------------------------------------------------------------------

type fakeGlobalReg struct {
	entries map[string]struct {
		p     pidapi.PID
		token uint64
	}
	nextToken uint64
}

func newFakeGlobalReg() *fakeGlobalReg {
	return &fakeGlobalReg{
		entries: make(map[string]struct {
			p     pidapi.PID
			token uint64
		}),
	}
}

func (r *fakeGlobalReg) Register(_ context.Context, name string, p pidapi.PID) (pidapi.PID, error) {
	r.nextToken++
	r.entries[name] = struct {
		p     pidapi.PID
		token uint64
	}{p: p, token: r.nextToken}
	return p, nil
}

func (r *fakeGlobalReg) Unregister(_ context.Context, name string) (bool, error) {
	delete(r.entries, name)
	return true, nil
}

func (r *fakeGlobalReg) Lookup(_ context.Context, name string, opts ...globalregapi.LookupOption) (globalregapi.LookupResult, error) {
	var o globalregapi.LookupOptions
	for _, opt := range opts {
		opt(&o)
	}
	if o.ByPID != nil {
		return globalregapi.LookupResult{PID: *o.ByPID}, nil
	}
	e, ok := r.entries[name]
	if !ok {
		return globalregapi.LookupResult{}, nil
	}
	res := globalregapi.LookupResult{PID: e.p, Found: true}
	if o.WithFence {
		res.FenceToken = e.token
	}
	return res, nil
}

func (r *fakeGlobalReg) LookupWithFence(name string) globalregapi.LookupResult {
	res, _ := r.Lookup(context.Background(), name, globalregapi.WithFence())
	return res
}

func (r *fakeGlobalReg) LookupByPID(_ pidapi.PID) []string { return nil }

func (r *fakeGlobalReg) ValidateFence(name string, token uint64) error {
	e, ok := r.entries[name]
	if !ok {
		return globalregapi.ErrStaleFence
	}
	if token < e.token {
		return globalregapi.ErrStaleFence
	}
	return nil
}

func (r *fakeGlobalReg) Remove(_ context.Context, _ pidapi.PID) error        { return nil }
func (r *fakeGlobalReg) RemoveNode(_ context.Context, _ pidapi.NodeID) error { return nil }

// ---------------------------------------------------------------------------
// fakeLocalReg implements topology.PIDRegistry for resolvePID tests.
// ---------------------------------------------------------------------------

type fakeLocalReg struct {
	entries map[string]pidapi.PID
}

func (r *fakeLocalReg) Register(name string, p pidapi.PID) (pidapi.PID, error) {
	if r.entries == nil {
		r.entries = make(map[string]pidapi.PID)
	}
	r.entries[name] = p
	return p, nil
}

func (r *fakeLocalReg) Lookup(name string) (pidapi.PID, bool) {
	if r.entries == nil {
		return pidapi.PID{}, false
	}
	p, ok := r.entries[name]
	return p, ok
}

func (r *fakeLocalReg) Unregister(_ string) bool { return true }
func (r *fakeLocalReg) Remove(_ pidapi.PID)      {}

// ---------------------------------------------------------------------------
// helper: create a Lua state wired up with both registries.
// ---------------------------------------------------------------------------

func newLuaWithRegistries(t *testing.T, gr *fakeGlobalReg, lr *fakeLocalReg) (*lua.LState, pidapi.PID) {
	t.Helper()
	l := lua.NewState()
	testPID := pidapi.PID{Host: "h1", UniqID: "sender1"}

	ctx := ctxapi.NewRootContext()
	security.SetStrictMode(ctx, false)
	globalregapi.WithRegistry(ctx, gr)
	topology.WithRegistry(ctx, lr)
	ctx, fc := ctxapi.OpenFrameContext(ctx)
	t.Cleanup(func() {
		CleanupFenceCache(l)
		ctxapi.ReleaseFrameContext(fc)
		l.Close()
	})
	require.NoError(t, runtimeapi.SetFramePID(ctx, testPID))
	l.SetContext(ctx)
	return l, testPID
}

// ---------------------------------------------------------------------------
// Integration tests for resolvePID
// ---------------------------------------------------------------------------

func TestResolvePID_GlobalNameReturnsFenceInfo(t *testing.T) {
	gr := newFakeGlobalReg()
	lr := &fakeLocalReg{}
	l, senderPID := newLuaWithRegistries(t, gr, lr)

	targetPID := pidapi.PID{Host: "h2", UniqID: "target1"}
	_, _ = gr.Register(context.Background(), "svc", targetPID)

	resolved, fi, err := resolvePID(l, "svc", "process.send", senderPID)
	require.NoError(t, err)
	assert.Equal(t, targetPID, resolved)
	require.NotNil(t, fi, "fenceInfo should not be nil for global name")
	assert.Equal(t, uint64(1), fi.fenceToken)
	assert.Equal(t, "svc", fi.globalName)
}

func TestResolvePID_RawPIDChecksFenceCache(t *testing.T) {
	gr := newFakeGlobalReg()
	lr := &fakeLocalReg{}
	l, senderPID := newLuaWithRegistries(t, gr, lr)

	targetPID := pidapi.PID{Host: "h2", UniqID: "target2"}
	cache := getFenceCache(l)
	cache[targetPID.String()] = fenceEntry{globalName: "cached-svc", fenceToken: 42}

	resolved, fi, err := resolvePID(l, targetPID.String(), "process.send", senderPID)
	require.NoError(t, err)
	assert.Equal(t, targetPID.String(), resolved.String())
	require.NotNil(t, fi, "fenceInfo should come from cache")
	assert.Equal(t, uint64(42), fi.fenceToken)
	assert.Equal(t, "cached-svc", fi.globalName)
}

func TestResolvePID_RawPIDWithoutCacheReturnsNilFence(t *testing.T) {
	gr := newFakeGlobalReg()
	lr := &fakeLocalReg{}
	l, senderPID := newLuaWithRegistries(t, gr, lr)

	targetPID := pidapi.PID{Host: "h3", UniqID: "target3"}

	resolved, fi, err := resolvePID(l, targetPID.String(), "process.send", senderPID)
	require.NoError(t, err)
	assert.Equal(t, targetPID.String(), resolved.String())
	assert.Nil(t, fi, "fenceInfo should be nil when PID is not in cache")
}

func TestResolvePID_GlobalLookupPopulatesFenceCache(t *testing.T) {
	gr := newFakeGlobalReg()
	lr := &fakeLocalReg{}
	l, senderPID := newLuaWithRegistries(t, gr, lr)

	targetPID := pidapi.PID{Host: "h4", UniqID: "target4"}
	_, _ = gr.Register(context.Background(), "svc", targetPID)

	_, _, err := resolvePID(l, "svc", "process.send", senderPID)
	require.NoError(t, err)

	cache := getFenceCache(l)
	entry, ok := cache[targetPID.String()]
	require.True(t, ok, "fence cache should contain entry for resolved PID")
	assert.Equal(t, "svc", entry.globalName)
	assert.Equal(t, uint64(1), entry.fenceToken)
}

func TestResolvePID_FallsBackToLocalRegistry(t *testing.T) {
	gr := newFakeGlobalReg()
	lr := &fakeLocalReg{}
	l, senderPID := newLuaWithRegistries(t, gr, lr)

	targetPID := pidapi.PID{Host: "h5", UniqID: "target5"}
	_, _ = lr.Register("local-svc", targetPID)

	resolved, fi, err := resolvePID(l, "local-svc", "process.send", senderPID)
	require.NoError(t, err)
	assert.Equal(t, targetPID, resolved)
	assert.Nil(t, fi, "fenceInfo should be nil for local registry lookup")
}
