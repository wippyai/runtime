// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	payloadapi "github.com/wippyai/runtime/api/payload"
	relayapi "github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/system/eventbus"
	"github.com/wippyai/runtime/system/payload"
	"github.com/wippyai/runtime/system/relay"
)

func TestClusterRejectsSplitNodeIdentity(t *testing.T) {
	hostname, err := os.Hostname()
	require.NoError(t, err)
	for _, tc := range []struct {
		name, clusterName, relayName string
		enabled, mismatch            bool
	}{
		{"explicit mismatch", "gossip", "relay", true, true},
		{"default mismatch", "", hostname + "-other", true, true},
		{"matching explicit", "same", "same", true, false},
		{"matching default", "", hostname, true, false},
		{"disabled", "gossip", "relay", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
			ctx = boot.WithConfig(ctx, boot.NewConfig(boot.WithSection("cluster", map[string]any{
				"enabled": tc.enabled, "name": tc.clusterName,
			})))
			ctx = event.WithBus(ctx, eventbus.NewBus())
			ctx = payloadapi.WithTranscoder(ctx, payload.NewTranscoder())
			ctx = relayapi.WithNode(ctx, relay.NewNode(tc.relayName))
			_, err := Cluster().Load(ctx)
			if tc.mismatch {
				require.ErrorContains(t, err, "must match relay.node_name")
				require.ErrorContains(t, err, tc.relayName)
				wantName := tc.clusterName
				if wantName == "" {
					wantName = hostname
				}
				require.ErrorContains(t, err, wantName)
			} else if tc.enabled {
				// Identity passed: the intentionally absent secret fails next.
				require.Error(t, err)
				require.NotContains(t, err.Error(), "must match relay.node_name")
			} else {
				require.NoError(t, err)
			}
		})
	}
}
