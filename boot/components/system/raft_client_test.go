// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/topology"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
)

// TestLoadClientRegistry_Guards pins the no-op guards that protect the prior
// client behavior (a role=client node wired no global registry). The happy-path
// wiring is proven end-to-end in cluster/clustertest (the forward/resolve path a
// client uses is identical there); here we only assert the guards never wire a
// registry — and never error — when they should not.
func TestLoadClientRegistry_Guards(t *testing.T) {
	cases := []struct {
		section map[string]any
		name    string
	}{
		{name: "fsm backend never wires a client registry", section: map[string]any{"raft.registry_backend": "fsm"}},
		{name: "kv backend no-ops without relay/membership prerequisites", section: map[string]any{"raft.registry_backend": "kv"}},
		{name: "default backend no-ops without prerequisites", section: map[string]any{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := boot.NewConfig(boot.WithSection(ClusterName, tc.section))
			out, err := loadClientRegistry(context.Background(), cfg.Sub(ClusterName), nil)
			require.NoError(t, err)
			require.Nil(t, globalapi.GetRegistry(out), "no global.Registry facade must be wired")
			require.Nil(t, topology.GetGlobalRegistry(out), "no topology.GlobalRegistry facade must be wired")
		})
	}
}
