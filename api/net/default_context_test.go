// SPDX-License-Identifier: MPL-2.0

package net

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
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
