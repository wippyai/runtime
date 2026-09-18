// SPDX-License-Identifier: MPL-2.0

package system

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
)

// TestInternodeAdvertiseEndpoint pins the v2 relay endpoint contract. The v1
// port remains separate in node metadata, preserving old-peer connectivity.
func TestInternodeAdvertiseEndpoint(t *testing.T) {
	cases := []struct {
		section                 map[string]any
		name, wantAddr, wantErr string
		wantPort                int
	}{
		{name: "defaults to v1 endpoint", section: map[string]any{}, wantPort: 7947},
		{name: "explicit relay endpoint", section: map[string]any{"internode.advertise_addr": "127.0.0.1", "internode.advertise_port": 19001}, wantAddr: "127.0.0.1", wantPort: 19001},
		{name: "default port with relay IP", section: map[string]any{"internode.advertise_addr": "127.0.0.1"}, wantAddr: "127.0.0.1", wantPort: 7947},
		{name: "accept DNS relay", section: map[string]any{"internode.advertise_addr": "proxy.internal", "internode.advertise_port": 19001}, wantAddr: "proxy.internal", wantPort: 19001},
		{name: "reject host with port", section: map[string]any{"internode.advertise_addr": "proxy.internal:19001"}, wantErr: "IP address or DNS hostname"},
		{name: "reject port without address", section: map[string]any{"internode.advertise_port": 19001}, wantErr: "requires advertise_addr"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := boot.NewConfig(boot.WithSection(ClusterName, tc.section)).Sub(ClusterName)
			addr, port, err := internodeAdvertiseEndpoint(cfg, 7947)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantAddr, addr)
			require.Equal(t, tc.wantPort, port)
		})
	}
}

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

func TestRaftBootstrapExpect(t *testing.T) {
	cases := []struct {
		section map[string]any
		name    string
		want    int
	}{
		{name: "local bootstrap default", section: map[string]any{}, want: 1},
		{name: "joining node default", section: map[string]any{"membership.join_addrs": "127.0.0.1:7946"}, want: 0},
		{name: "blank seed is local", section: map[string]any{"membership.join_addrs": "  "}, want: 1},
		{name: "explicit bootstrap wins", section: map[string]any{"membership.join_addrs": "127.0.0.1:7946", "raft.bootstrap_expect": 3}, want: 3},
		{name: "explicit zero", section: map[string]any{"raft.bootstrap_expect": 0}, want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := boot.NewConfig(boot.WithSection(ClusterName, tc.section))
			require.Equal(t, tc.want, raftBootstrapExpect(cfg.Sub(ClusterName)))
		})
	}
}

// TestClusterSeedAddrs pins the single reading of membership.join_addrs that
// both the membership service and raft bootstrap sizing depend on.
func TestClusterSeedAddrs(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  []string
	}{
		{name: "unset", value: "", want: nil},
		{name: "whitespace only", value: "  ", want: nil},
		{name: "blank entry dropped", value: "a, ,b", want: []string{"a", "b"}},
		{name: "entries trimmed", value: " a , b ", want: []string{"a", "b"}},
		{name: "single seed", value: "127.0.0.1:7946", want: []string{"127.0.0.1:7946"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := boot.NewConfig(boot.WithSection(ClusterName, map[string]any{
				ClusterMembershipJoin: tc.value,
			})).Sub(ClusterName)
			seeds := clusterSeedAddrs(cfg)
			require.Equal(t, tc.want, seeds)
			wantBootstrap := 1
			if len(tc.want) > 0 {
				wantBootstrap = 0
			}
			require.Equal(t, wantBootstrap, raftBootstrapExpect(cfg),
				"raft bootstrap sizing must read the same seed list")
		})
	}
}
