// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/api/topology/namereg/global"
)

// fakeGlobalReg is an in-memory global.Registry used to drive the
// global-name branch of ResolveDestination.
type fakeGlobalReg struct {
	entries map[string]pidapi.PID
}

func newFakeGlobalReg() *fakeGlobalReg {
	return &fakeGlobalReg{entries: make(map[string]pidapi.PID)}
}

func (r *fakeGlobalReg) Register(_ context.Context, name string, p pidapi.PID) (pidapi.PID, error) {
	r.entries[name] = p
	return p, nil
}

func (r *fakeGlobalReg) RegisterScope(ctx context.Context, name string, p pidapi.PID, _ global.RegistrationMode) (global.RegisterOutcome, error) {
	out, err := r.Register(ctx, name, p)
	return global.RegisterOutcome{PID: out, State: global.RegisterStateActive}, err
}

func (r *fakeGlobalReg) UnregisterScope(ctx context.Context, name string, _ global.RegistrationMode) (bool, error) {
	return r.Unregister(ctx, name)
}

func (r *fakeGlobalReg) Unregister(_ context.Context, name string) (bool, error) {
	if _, ok := r.entries[name]; !ok {
		return false, nil
	}
	delete(r.entries, name)
	return true, nil
}

func (r *fakeGlobalReg) Lookup(_ context.Context, name string, opts ...global.LookupOption) (global.LookupResult, error) {
	var o global.LookupOptions
	for _, opt := range opts {
		opt(&o)
	}
	if o.ByPID != nil {
		return global.LookupResult{PID: *o.ByPID}, nil
	}
	p, ok := r.entries[name]
	if !ok {
		return global.LookupResult{}, nil
	}
	return global.LookupResult{PID: p, Found: true}, nil
}

func (r *fakeGlobalReg) LookupByPID(_ pidapi.PID) []string { return nil }

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

func (r *fakeEventualReg) Register(name string, p pidapi.PID) (pidapi.PID, error) {
	r.entries[name] = p
	return p, nil
}

func (r *fakeEventualReg) Unregister(name string) bool {
	if _, ok := r.entries[name]; !ok {
		return false
	}
	delete(r.entries, name)
	return true
}

func (r *fakeEventualReg) Lookup(_ context.Context, name string, _ ...global.LookupOption) (global.LookupResult, error) {
	p, ok := r.entries[name]
	if !ok {
		return global.LookupResult{}, nil
	}
	return global.LookupResult{PID: p, Found: true}, nil
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
		global.WithRegistry(ctx, gr)
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
}

func TestResolveDestination_GlobalName(t *testing.T) {
	gr := newFakeGlobalReg()
	target := pidapi.PID{Host: "h", UniqID: "global1"}
	_, _ = gr.Register(context.Background(), "svc.global", target)
	ctx := buildCtx(gr, nil, nil)

	resolved, err := ResolveDestination(ctx, "svc.global")
	require.NoError(t, err)
	assert.Equal(t, target, resolved.PID)
}

func TestResolveDestination_EventualName(t *testing.T) {
	er := newFakeEventualReg()
	target := pidapi.PID{Host: "h", UniqID: "eventual1"}
	er.put("svc.eventual", target)
	ctx := buildCtx(nil, er, nil)

	resolved, err := ResolveDestination(ctx, "svc.eventual")
	require.NoError(t, err)
	assert.Equal(t, target, resolved.PID)
}

func TestResolveDestination_LocalName(t *testing.T) {
	lr := newFakeLocalReg()
	target := pidapi.PID{Host: "h", UniqID: "local1"}
	_, _ = lr.Register("svc.local", target)
	ctx := buildCtx(nil, nil, lr)

	resolved, err := ResolveDestination(ctx, "svc.local")
	require.NoError(t, err)
	assert.Equal(t, target, resolved.PID)
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
}
