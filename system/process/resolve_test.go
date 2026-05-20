// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/globalreg"
	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/topology"
)

// fakeGlobalReg is an in-memory globalreg.Registry used to drive the
// fence-bearing branch of ResolveDestination. Each successful Register
// monotonically increments the fence token.
type fakeGlobalReg struct {
	entries   map[string]fakeEntry
	nextToken uint64
}

type fakeEntry struct {
	p     pidapi.PID
	token uint64
}

func newFakeGlobalReg() *fakeGlobalReg {
	return &fakeGlobalReg{entries: make(map[string]fakeEntry)}
}

func (r *fakeGlobalReg) Register(_ context.Context, name string, p pidapi.PID) (pidapi.PID, error) {
	r.nextToken++
	r.entries[name] = fakeEntry{p: p, token: r.nextToken}
	return p, nil
}

func (r *fakeGlobalReg) Unregister(_ context.Context, name string) (bool, error) {
	if _, ok := r.entries[name]; !ok {
		return false, nil
	}
	delete(r.entries, name)
	return true, nil
}

func (r *fakeGlobalReg) Lookup(_ context.Context, name string, opts ...globalreg.LookupOption) (globalreg.LookupResult, error) {
	var o globalreg.LookupOptions
	for _, opt := range opts {
		opt(&o)
	}
	if o.ByPID != nil {
		return globalreg.LookupResult{PID: *o.ByPID}, nil
	}
	e, ok := r.entries[name]
	if !ok {
		return globalreg.LookupResult{}, nil
	}
	res := globalreg.LookupResult{PID: e.p, Found: true}
	if o.WithFence {
		res.FenceToken = e.token
	}
	return res, nil
}

func (r *fakeGlobalReg) LookupWithFence(name string) globalreg.LookupResult {
	res, _ := r.Lookup(context.Background(), name, globalreg.WithFence())
	return res
}

func (r *fakeGlobalReg) LookupByPID(_ pidapi.PID) []string { return nil }

func (r *fakeGlobalReg) ValidateFence(name string, token uint64) error {
	e, ok := r.entries[name]
	if !ok || token < e.token {
		return globalreg.ErrStaleFence
	}
	return nil
}

func (r *fakeGlobalReg) Remove(_ context.Context, _ pidapi.PID) error        { return nil }
func (r *fakeGlobalReg) RemoveNode(_ context.Context, _ pidapi.NodeID) error { return nil }

// fakeEventualReg implements topology.EventualRegistry with no fencing.
type fakeEventualReg struct {
	entries map[string]pidapi.PID
}

func newFakeEventualReg() *fakeEventualReg {
	return &fakeEventualReg{entries: make(map[string]pidapi.PID)}
}

func (r *fakeEventualReg) put(name string, p pidapi.PID) {
	r.entries[name] = p
}

func (r *fakeEventualReg) Lookup(_ context.Context, name string, _ ...globalreg.LookupOption) (globalreg.LookupResult, error) {
	p, ok := r.entries[name]
	if !ok {
		return globalreg.LookupResult{}, nil
	}
	return globalreg.LookupResult{PID: p, Found: true}, nil
}

// fakeLocalReg implements topology.PIDRegistry without consulting any of
// the cluster registries — this isolates the local fallback branch.
type fakeLocalReg struct {
	entries map[string]pidapi.PID
}

func newFakeLocalReg() *fakeLocalReg {
	return &fakeLocalReg{entries: make(map[string]pidapi.PID)}
}

func (r *fakeLocalReg) Register(name string, p pidapi.PID) (pidapi.PID, error) {
	r.entries[name] = p
	return p, nil
}

func (r *fakeLocalReg) Lookup(name string) (pidapi.PID, bool) {
	p, ok := r.entries[name]
	return p, ok
}

func (r *fakeLocalReg) Unregister(_ string) bool { return true }
func (r *fakeLocalReg) Remove(_ pidapi.PID)      {}

// buildCtx returns a fresh context with the three registries installed.
func buildCtx(gr *fakeGlobalReg, er *fakeEventualReg, lr *fakeLocalReg) context.Context {
	ctx := ctxapi.NewRootContext()
	if gr != nil {
		globalreg.WithRegistry(ctx, gr)
	}
	if er != nil {
		topology.WithEventualRegistry(ctx, er)
	}
	if lr != nil {
		topology.WithRegistry(ctx, lr)
	}
	return ctx
}

func TestResolveDestination_RawPID(t *testing.T) {
	ctx := buildCtx(nil, nil, nil)

	want := pidapi.PID{Host: "h", UniqID: "u1"}
	resolved, err := ResolveDestination(ctx, want.String())
	require.NoError(t, err)
	assert.Equal(t, want.String(), resolved.PID.String())
	assert.Empty(t, resolved.GlobalName)
	assert.Zero(t, resolved.FenceToken)
}

func TestResolveDestination_GlobalNameCarriesFence(t *testing.T) {
	gr := newFakeGlobalReg()
	target := pidapi.PID{Host: "h", UniqID: "global1"}
	_, _ = gr.Register(context.Background(), "svc.global", target)
	ctx := buildCtx(gr, nil, nil)

	resolved, err := ResolveDestination(ctx, "svc.global")
	require.NoError(t, err)
	assert.Equal(t, target, resolved.PID)
	assert.Equal(t, "svc.global", resolved.GlobalName)
	assert.Equal(t, uint64(1), resolved.FenceToken)
}

func TestResolveDestination_EventualNameNoFence(t *testing.T) {
	er := newFakeEventualReg()
	target := pidapi.PID{Host: "h", UniqID: "eventual1"}
	er.put("svc.eventual", target)
	ctx := buildCtx(nil, er, nil)

	resolved, err := ResolveDestination(ctx, "svc.eventual")
	require.NoError(t, err)
	assert.Equal(t, target, resolved.PID)
	assert.Empty(t, resolved.GlobalName, "eventual lookups must not surface fence/global metadata")
	assert.Zero(t, resolved.FenceToken)
}

func TestResolveDestination_LocalNameNoFence(t *testing.T) {
	lr := newFakeLocalReg()
	target := pidapi.PID{Host: "h", UniqID: "local1"}
	_, _ = lr.Register("svc.local", target)
	ctx := buildCtx(nil, nil, lr)

	resolved, err := ResolveDestination(ctx, "svc.local")
	require.NoError(t, err)
	assert.Equal(t, target, resolved.PID)
	assert.Empty(t, resolved.GlobalName)
	assert.Zero(t, resolved.FenceToken)
}

func TestResolveDestination_NotFound(t *testing.T) {
	ctx := buildCtx(newFakeGlobalReg(), newFakeEventualReg(), newFakeLocalReg())

	_, err := ResolveDestination(ctx, "nope")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrCouldNotResolve))
}

func TestResolveDestination_GlobalShadowsEventualAndLocal(t *testing.T) {
	gr := newFakeGlobalReg()
	er := newFakeEventualReg()
	lr := newFakeLocalReg()

	globalPID := pidapi.PID{Host: "h", UniqID: "global"}
	eventualPID := pidapi.PID{Host: "h", UniqID: "eventual"}
	localPID := pidapi.PID{Host: "h", UniqID: "local"}

	_, _ = gr.Register(context.Background(), "svc", globalPID)
	er.put("svc", eventualPID)
	_, _ = lr.Register("svc", localPID)

	ctx := buildCtx(gr, er, lr)
	resolved, err := ResolveDestination(ctx, "svc")
	require.NoError(t, err)
	assert.Equal(t, globalPID, resolved.PID, "global registration must win")
	assert.Equal(t, "svc", resolved.GlobalName)
	assert.Equal(t, uint64(1), resolved.FenceToken)
}

func TestResolveDestination_FenceMonotonicAcrossReRegister(t *testing.T) {
	gr := newFakeGlobalReg()
	ctx := buildCtx(gr, nil, nil)

	pidA := pidapi.PID{Host: "h", UniqID: "a"}
	_, _ = gr.Register(context.Background(), "svc.race", pidA)

	first, err := ResolveDestination(ctx, "svc.race")
	require.NoError(t, err)
	assert.Equal(t, pidA, first.PID)
	assert.Equal(t, uint64(1), first.FenceToken)

	pidB := pidapi.PID{Host: "h", UniqID: "b"}
	_, _ = gr.Unregister(context.Background(), "svc.race")
	_, _ = gr.Register(context.Background(), "svc.race", pidB)

	second, err := ResolveDestination(ctx, "svc.race")
	require.NoError(t, err)
	assert.Equal(t, pidB, second.PID, "re-registration must surface the new PID")
	assert.Greater(t, second.FenceToken, first.FenceToken,
		"fence token must monotonically advance across re-registration")
}
