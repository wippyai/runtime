// SPDX-License-Identifier: MPL-2.0

package net

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/registry"
)

// openFrame is a small helper that creates a frame on the given context.
func openFrame(parent context.Context, t *testing.T) (context.Context, ctxapi.FrameContext) {
	t.Helper()
	ctx, fc := ctxapi.OpenFrameContext(parent)
	require.NotNil(t, fc)
	return ctx, fc
}

// setFrameDefault writes the default network onto a frame using the same
// pair-injection path the dispatchers use in production.
func setFrameDefault(t *testing.T, fc ctxapi.FrameContext, id string) {
	t.Helper()
	require.NoError(t, fc.SetMultiple(DefaultNetworkPair(id)))
}

// --- GetDefaultNetwork ---

func TestGetDefaultNetwork_NoFrame(t *testing.T) {
	assert.Equal(t, "", GetDefaultNetwork(context.Background()))
}

func TestGetDefaultNetwork_UnsetInFrame(t *testing.T) {
	ctx, _ := openFrame(context.Background(), t)
	assert.Equal(t, "", GetDefaultNetwork(ctx))
}

// --- DefaultNetworkPair injection ---

func TestDefaultNetworkPair_SetAndGet(t *testing.T) {
	ctx, fc := openFrame(context.Background(), t)
	setFrameDefault(t, fc, "app.net:socks5")

	assert.Equal(t, "app.net:socks5", GetDefaultNetwork(ctx))
}

func TestDefaultNetwork_InheritsToForkedFrame(t *testing.T) {
	parentCtx, parent := openFrame(context.Background(), t)
	setFrameDefault(t, parent, "app.net:socks5")
	parent.Seal()

	childCtx, child := ctxapi.OpenFrameContext(parentCtx)
	require.NotNil(t, child)
	require.NotSame(t, parent, child, "child must be a new frame after parent sealed")

	assert.Equal(t, "app.net:socks5", GetDefaultNetwork(childCtx),
		"Inherit:true key must auto-propagate to forked child frame")
}

func TestDefaultNetwork_ChildOverrideDoesNotAffectParent(t *testing.T) {
	parentCtx, parent := openFrame(context.Background(), t)
	setFrameDefault(t, parent, "app.net:socks5")
	parent.Seal()

	childCtx, child := ctxapi.OpenFrameContext(parentCtx)
	setFrameDefault(t, child, "app.net:tailscale")

	assert.Equal(t, "app.net:tailscale", GetDefaultNetwork(childCtx))
	assert.Equal(t, "app.net:socks5", GetDefaultNetwork(parentCtx),
		"parent frame must retain its original value after child override")
}

func TestDefaultNetworkKey_InheritFlag(t *testing.T) {
	require.True(t, defaultNetworkKey.Inherit,
		"defaultNetworkKey must be Inherit:true — otherwise child frames lose the network selection")
}

func TestDefaultNetwork_DeepFork(t *testing.T) {
	parentCtx, parent := openFrame(context.Background(), t)
	setFrameDefault(t, parent, "app.net:socks5")
	parent.Seal()

	childCtx, child := ctxapi.OpenFrameContext(parentCtx)
	assert.Equal(t, "app.net:socks5", GetDefaultNetwork(childCtx))
	child.Seal()

	grandCtx, _ := ctxapi.OpenFrameContext(childCtx)
	assert.Equal(t, "app.net:socks5", GetDefaultNetwork(grandCtx),
		"network should survive grandchild fork")
}

// --- App-level default on AppContext ---

func TestAppDefaultNetwork_NoAppContext(t *testing.T) {
	assert.Equal(t, "", AppDefaultNetwork(context.Background()))
}

func TestAppDefaultNetwork_UnsetAppContext(t *testing.T) {
	ac := ctxapi.NewAppContext()
	ctx := ctxapi.WithAppContext(context.Background(), ac)
	assert.Equal(t, "", AppDefaultNetwork(ctx))
}

func TestWithAppDefaultNetwork_SetAndGet(t *testing.T) {
	ac := ctxapi.NewAppContext()
	ctx := ctxapi.WithAppContext(context.Background(), ac)

	ctx = WithAppDefaultNetwork(ctx, "app.net:socks5")
	assert.Equal(t, "app.net:socks5", AppDefaultNetwork(ctx))
}

func TestWithAppDefaultNetwork_NoAppContext_Noop(t *testing.T) {
	ctx := WithAppDefaultNetwork(context.Background(), "app.net:socks5")
	assert.Equal(t, "", AppDefaultNetwork(ctx))
}

func TestWithAppDefaultNetwork_AppDefaultDoesNotReachFrame(t *testing.T) {
	// Setting the app-level default must not populate the frame-level key.
	// Dispatchers are responsible for copying the app default into the
	// frame via DefaultNetworkPair on task spawn.
	ac := ctxapi.NewAppContext()
	ctx := ctxapi.WithAppContext(context.Background(), ac)
	ctx = WithAppDefaultNetwork(ctx, "app.net:socks5")

	ctx, _ = openFrame(ctx, t)
	assert.Equal(t, "", GetDefaultNetwork(ctx),
		"app-level default must not leak into FrameContext directly")
	assert.Equal(t, "app.net:socks5", AppDefaultNetwork(ctx))
}

// --- ApplyOverlayPair (centralized spawn-site helper) ---

// stubRegistry implements NetworkRegistry with an in-memory set of IDs.
type stubRegistry struct {
	known map[string]struct{}
}

func newStubRegistry(ids ...string) *stubRegistry {
	r := &stubRegistry{known: make(map[string]struct{}, len(ids))}
	for _, id := range ids {
		r.known[id] = struct{}{}
	}
	return r
}

func (r *stubRegistry) GetNetwork(id registry.ID) (Service, error) {
	if _, ok := r.known[id.String()]; !ok {
		return nil, ErrNetworkNotFound
	}
	return nil, nil
}

func (r *stubRegistry) HasNetwork(id registry.ID) bool {
	_, ok := r.known[id.String()]
	return ok
}

func (r *stubRegistry) NetworkKind(_ registry.ID) registry.Kind { return KindSOCKS5 }

// ctxWithRegistry returns a fresh AppContext populated with reg.
func ctxWithRegistry(reg NetworkRegistry) context.Context {
	ctx := ctxapi.NewRootContext()
	if reg != nil {
		ctx = WithNetworkRegistry(ctx, reg)
	}
	return ctx
}

// optsWithNetwork returns an attrs.Bag containing the network option.
func optsWithNetwork(id string) attrs.Attributes {
	b := attrs.NewBag()
	b.Set(OptionKeyNetwork, id)
	return b
}

func TestApplyOverlayPair_NoSelection_Passthrough(t *testing.T) {
	ctx := ctxWithRegistry(newStubRegistry())
	pairs := []ctxapi.Pair{}

	out, err := ApplyOverlayPair(ctx, nil, pairs)
	require.NoError(t, err)
	assert.Empty(t, out, "no selection must leave pairs unchanged")
}

func TestApplyOverlayPair_EmptyOptions_Passthrough(t *testing.T) {
	ctx := ctxWithRegistry(newStubRegistry())
	empty := attrs.NewBag()

	out, err := ApplyOverlayPair(ctx, empty, nil)
	require.NoError(t, err)
	assert.Nil(t, out, "empty options with nil pairs must round-trip nil")
}

func TestApplyOverlayPair_OptionsSelection_Appends(t *testing.T) {
	ctx := ctxWithRegistry(newStubRegistry("app.net:socks5"))

	out, err := ApplyOverlayPair(ctx, optsWithNetwork("app.net:socks5"), nil)
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, DefaultNetworkPair("app.net:socks5"), out[0])
}

func TestApplyOverlayPair_AppDefaultFallback_Appends(t *testing.T) {
	ctx := ctxWithRegistry(newStubRegistry("app.net:socks5"))
	ctx = WithAppDefaultNetwork(ctx, "app.net:socks5")

	out, err := ApplyOverlayPair(ctx, nil, nil)
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, "app.net:socks5", out[0].Value)
}

func TestApplyOverlayPair_OptionsBeatsAppDefault(t *testing.T) {
	ctx := ctxWithRegistry(newStubRegistry("app.net:tailscale"))
	ctx = WithAppDefaultNetwork(ctx, "app.net:socks5")

	out, err := ApplyOverlayPair(ctx, optsWithNetwork("app.net:tailscale"), nil)
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, "app.net:tailscale", out[0].Value,
		"per-call options must override app-level default")
}

func TestApplyOverlayPair_UnknownNetwork_Errors(t *testing.T) {
	ctx := ctxWithRegistry(newStubRegistry("app.net:other"))

	out, err := ApplyOverlayPair(ctx, optsWithNetwork("app.net:missing"), nil)
	assert.True(t, errors.Is(err, ErrNetworkNotFound))
	assert.Nil(t, out)
}

func TestApplyOverlayPair_NoRegistry_Errors(t *testing.T) {
	// Selection present, but no registry on the AppContext to verify it.
	ctx := ctxapi.NewRootContext()

	out, err := ApplyOverlayPair(ctx, optsWithNetwork("app.net:socks5"), nil)
	assert.True(t, errors.Is(err, ErrNetworkNotFound))
	assert.Nil(t, out)
}

func TestApplyOverlayPair_PreservesExistingPairs(t *testing.T) {
	ctx := ctxWithRegistry(newStubRegistry("app.net:socks5"))

	marker := &ctxapi.Key{Name: "test.marker"}
	existing := []ctxapi.Pair{{Key: marker, Value: "sentinel"}}

	out, err := ApplyOverlayPair(ctx, optsWithNetwork("app.net:socks5"), existing)
	require.NoError(t, err)
	require.Len(t, out, 2)
	assert.Equal(t, "sentinel", out[0].Value, "prior pairs must be preserved")
	assert.Equal(t, "app.net:socks5", out[1].Value, "overlay pair must be appended")
}
