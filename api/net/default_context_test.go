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

func TestGetDefaultNetwork_NoFrame(t *testing.T) {
	assert.Equal(t, "", GetDefaultNetwork(context.Background()))
}

func TestGetDefaultNetwork_UnsetInFrame(t *testing.T) {
	ctx, _ := openFrame(context.Background(), t)
	assert.Equal(t, "", GetDefaultNetwork(ctx))
}

func TestSetDefaultNetwork_SetAndGet(t *testing.T) {
	ctx, _ := openFrame(context.Background(), t)

	require.NoError(t, SetDefaultNetwork(ctx, "app.net:socks5"))
	assert.Equal(t, "app.net:socks5", GetDefaultNetwork(ctx))
}

func TestSetDefaultNetwork_NoFrame_IsNoop(t *testing.T) {
	assert.NoError(t, SetDefaultNetwork(context.Background(), "app.net:socks5"))
}

// TestDefaultNetwork_InheritsToForkedFrame verifies the Inherit:true contract.
// A child frame forked after the parent is sealed must see the parent's
// default network without any explicit copy.
func TestDefaultNetwork_InheritsToForkedFrame(t *testing.T) {
	parentCtx, parent := openFrame(context.Background(), t)
	require.NoError(t, SetDefaultNetwork(parentCtx, "app.net:socks5"))

	parent.Seal()

	childCtx, child := ctxapi.OpenFrameContext(parentCtx)
	require.NotNil(t, child)
	require.NotSame(t, parent, child, "child must be a new frame after parent sealed")

	assert.Equal(t, "app.net:socks5", GetDefaultNetwork(childCtx),
		"Inherit:true key must auto-propagate to forked child frame")
}

// TestDefaultNetwork_ChildOverrideDoesNotAffectParent verifies that writes on
// a forked child frame do not leak back into the sealed parent.
func TestDefaultNetwork_ChildOverrideDoesNotAffectParent(t *testing.T) {
	parentCtx, parent := openFrame(context.Background(), t)
	require.NoError(t, SetDefaultNetwork(parentCtx, "app.net:socks5"))
	parent.Seal()

	childCtx, _ := ctxapi.OpenFrameContext(parentCtx)
	require.NoError(t, SetDefaultNetwork(childCtx, "app.net:tailscale"))

	assert.Equal(t, "app.net:tailscale", GetDefaultNetwork(childCtx))
	assert.Equal(t, "app.net:socks5", GetDefaultNetwork(parentCtx),
		"parent frame must retain its original value after child override")
}

// TestDefaultNetwork_CrossesForkViaPair verifies that the pair produced by
// DefaultNetworkPair carries the Inherit flag and is applied correctly when
// injected via SetMultiple (as lifecycle.go does when spawning children).
func TestDefaultNetwork_CrossesForkViaPair(t *testing.T) {
	parentCtx, parent := openFrame(context.Background(), t)

	pair := DefaultNetworkPair("app.net:socks5")
	require.NoError(t, parent.SetMultiple(pair))

	assert.Equal(t, "app.net:socks5", GetDefaultNetwork(parentCtx))

	parent.Seal()
	childCtx, _ := ctxapi.OpenFrameContext(parentCtx)

	assert.Equal(t, "app.net:socks5", GetDefaultNetwork(childCtx),
		"pair-based injection must also inherit to child frame")
}

// TestDefaultNetworkKey_InheritFlag is the structural contract check: the key
// MUST be marked Inherit:true for the frame-fork copy loop to propagate it.
// If this test ever fails, child processes/functions will lose their parent's
// network selection.
func TestDefaultNetworkKey_InheritFlag(t *testing.T) {
	require.True(t, defaultNetworkKey.Inherit,
		"defaultNetworkKey must be Inherit:true — otherwise child frames lose the network selection")
}

// TestDefaultNetwork_DeepFork verifies that inheritance works through multiple
// fork generations (parent -> child -> grandchild), matching how a function
// that spawns a process (which itself spawns a nested process) should behave.
func TestDefaultNetwork_DeepFork(t *testing.T) {
	parentCtx, parent := openFrame(context.Background(), t)
	require.NoError(t, SetDefaultNetwork(parentCtx, "app.net:socks5"))
	parent.Seal()

	childCtx, child := ctxapi.OpenFrameContext(parentCtx)
	assert.Equal(t, "app.net:socks5", GetDefaultNetwork(childCtx))
	child.Seal()

	grandCtx, _ := ctxapi.OpenFrameContext(childCtx)
	assert.Equal(t, "app.net:socks5", GetDefaultNetwork(grandCtx),
		"network should survive grandchild fork")
}
