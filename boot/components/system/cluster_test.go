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
		name    string
		section map[string]any
		want    bool
	}{
		{"defaults (no raft.* set)", map[string]any{}, true},
		{"role server", map[string]any{"raft.role": "server"}, true},
		{"role client", map[string]any{"raft.role": "client"}, false},
		{"role client mixed case", map[string]any{"raft.role": "Client"}, false},
		{"enabled false", map[string]any{"raft.enabled": false}, false},
		{"enabled true role server", map[string]any{"raft.enabled": true, "raft.role": "server"}, true},
		{"enabled true role client", map[string]any{"raft.enabled": true, "raft.role": "client"}, false},
		{"enabled false role server", map[string]any{"raft.enabled": false, "raft.role": "server"}, false},
		{"unknown role treated as server", map[string]any{"raft.role": "voter"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := boot.NewConfig(boot.WithSection(string(ClusterName), tc.section))
			require.Equal(t, tc.want, clusterRaftEnabled(cfg.Sub(ClusterName)))
		})
	}
}
