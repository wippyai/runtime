// SPDX-License-Identifier: MPL-2.0

package system

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
)

// TestClusterRaftEnabled_RoleComposition pins the rule that role and
// enabled compose with AND: a node runs raft only when enabled is true
// AND role is not "client", so no combination of the two knobs can
// contradict (any "off" wins).
func TestClusterRaftEnabled_RoleComposition(t *testing.T) {
	cases := []struct {
		section map[string]any
		name    string
		want    bool
	}{
		{name: "defaults (no raft.* set)", section: map[string]any{}, want: true},
		{name: "role server", section: map[string]any{"raft.role": "server"}, want: true},
		{name: "role client", section: map[string]any{"raft.role": "client"}, want: false},
		{name: "role client mixed case", section: map[string]any{"raft.role": "Client"}, want: false},
		{name: "enabled false", section: map[string]any{"raft.enabled": false}, want: false},
		{name: "enabled true role server", section: map[string]any{"raft.enabled": true, "raft.role": "server"}, want: true},
		{name: "enabled true role client", section: map[string]any{"raft.enabled": true, "raft.role": "client"}, want: false},
		{name: "enabled false role server", section: map[string]any{"raft.enabled": false, "raft.role": "server"}, want: false},
		{name: "unknown role treated as server", section: map[string]any{"raft.role": "voter"}, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := boot.NewConfig(boot.WithSection(ClusterName, tc.section))
			require.Equal(t, tc.want, clusterRaftEnabled(cfg.Sub(ClusterName)))
		})
	}
}
