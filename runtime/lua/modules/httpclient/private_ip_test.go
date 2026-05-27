// SPDX-License-Identifier: MPL-2.0

package httpclient

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	netapi "github.com/wippyai/runtime/api/net"
)

func TestHasOverlay_ExplicitOption(t *testing.T) {
	ctx := context.Background()
	opts := &requestOptions{overlayNetwork: "network:tor"}
	assert.True(t, hasOverlay(ctx, opts))
}

func TestHasOverlay_FrameDefault(t *testing.T) {
	// Frame-level default (injected via DefaultNetworkPair) must count as
	// an overlay — otherwise the Lua client would perform local DNS for
	// hostnames destined for the overlay and leak them to the system
	// resolver.
	ctx := ctxapi.NewRootContext()
	ctx, fc := ctxapi.OpenFrameContext(ctx)
	defer ctxapi.ReleaseFrameContext(fc)
	require.NoError(t, fc.SetMultiple(netapi.DefaultNetworkPair("network:tor")))

	opts := &requestOptions{}
	assert.True(t, hasOverlay(ctx, opts), "frame default must activate overlay gating")
}

func TestHasOverlay_None(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	opts := &requestOptions{}
	assert.False(t, hasOverlay(ctx, opts))
}

func TestCheckPrivateIP_FrameDefaultSkipsDNS(t *testing.T) {
	// When a frame default overlay is set, the Lua client must not resolve
	// the target hostname locally — the overlay resolves DNS on the remote
	// side. We pick a host that would resolve to a public IP (or fail) to
	// confirm checkPrivateIP returns without any DNS activity.
	ctx := ctxapi.NewRootContext()
	ctx, fc := ctxapi.OpenFrameContext(ctx)
	defer ctxapi.ReleaseFrameContext(fc)
	require.NoError(t, fc.SetMultiple(netapi.DefaultNetworkPair("network:tor")))

	opts := &requestOptions{}
	got := checkPrivateIP(ctx, "http://some-hidden-service.onion/path", hasOverlay(ctx, opts))
	assert.Empty(t, got, "overlay must short-circuit private-IP check for hostnames")
}

func TestCheckPrivateIP_LiteralPrivateIPStillBlocked(t *testing.T) {
	// Overlay gating must not relax the literal-IP check — sending a
	// private IP through an overlay would still reach a service on the
	// exit node's LAN.
	ctx := ctxapi.NewRootContext()

	opts := &requestOptions{overlayNetwork: "network:tor"}
	got := checkPrivateIP(ctx, "http://127.0.0.1/", hasOverlay(ctx, opts))
	assert.Contains(t, got, "not allowed: private IP 127.0.0.1")
}
