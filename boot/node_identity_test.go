// SPDX-License-Identifier: MPL-2.0

package boot

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	relayapi "github.com/wippyai/runtime/api/relay"
	"go.uber.org/zap"
)

func TestBootstrapClusterAndRelayIdentity(t *testing.T) {
	t.Setenv("WIPPY_NODE_ID", "environment-node")
	for _, tc := range []struct {
		name, cluster, relay, want string
		enabled, conflict          bool
	}{
		{name: "cluster config", enabled: true, cluster: "named-node", want: "named-node"},
		{name: "relay config", enabled: true, relay: "relay-node", want: "relay-node"},
		{name: "matching config", enabled: true, cluster: "same", relay: "same", want: "same"},
		{name: "default", enabled: true, want: "environment-node"},
		{name: "disabled cluster", cluster: "unused", want: "environment-node"},
		{name: "conflicting config", enabled: true, cluster: "cluster-node", relay: "relay-node", conflict: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := boot.NewConfig(boot.WithSection("cluster", map[string]any{"enabled": tc.enabled, "name": tc.cluster}), boot.WithSection("relay", map[string]any{"node_name": tc.relay}))
			ctx, err := NewBootstrapContext(zap.NewNop(), cfg)
			if ctx != nil {
				t.Cleanup(func() { event.GetBus(ctx).(interface{ Stop() }).Stop() })
			}
			if tc.conflict {
				require.ErrorContains(t, err, "identity")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, relayapi.GetNode(ctx).ID())
		})
	}
}
