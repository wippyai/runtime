// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"testing"
	"time"

	hraft "github.com/hashicorp/raft"
	"github.com/stretchr/testify/require"
	raftapi "github.com/wippyai/runtime/api/raft"
)

// TestConfigInvariants_AcceptedByHraft feeds adversarial operator timeout
// combinations through InitDefaults + toHashicorpConfig and asserts the
// result always passes hashicorp/raft's own ValidateConfig. This pins the
// guarantee that no operator-settable timeout combination can produce a
// config NewRaft would reject (which would fail the node's boot):
//   - ElectionTimeout >= HeartbeatTimeout (clamped in InitDefaults),
//   - LeaderLeaseTimeout <= HeartbeatTimeout (coupled in toHashicorpConfig).
func TestConfigInvariants_AcceptedByHraft(t *testing.T) {
	cases := []struct {
		name string
		cfg  raftapi.Config
	}{
		{"all-defaults", raftapi.Config{}},
		{"heartbeat-raised-election-unset", raftapi.Config{HeartbeatTimeout: 5 * time.Second}},
		{"heartbeat-raised-high", raftapi.Config{HeartbeatTimeout: 30 * time.Second}},
		{"election-below-heartbeat", raftapi.Config{HeartbeatTimeout: 4 * time.Second, ElectionTimeout: 1 * time.Second}},
		{"heartbeat-tiny", raftapi.Config{HeartbeatTimeout: 10 * time.Millisecond}},
		{"both-equal", raftapi.Config{HeartbeatTimeout: 2 * time.Second, ElectionTimeout: 2 * time.Second}},
		{"election-far-above", raftapi.Config{HeartbeatTimeout: 1 * time.Second, ElectionTimeout: 20 * time.Second}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.cfg
			cfg.InitDefaults()
			require.GreaterOrEqual(t, cfg.ElectionTimeout, cfg.HeartbeatTimeout,
				"InitDefaults must enforce ElectionTimeout >= HeartbeatTimeout")

			rc := toHashicorpConfig("node-1", cfg)
			require.LessOrEqual(t, rc.LeaderLeaseTimeout, rc.HeartbeatTimeout,
				"LeaderLeaseTimeout must not exceed HeartbeatTimeout")
			require.NoError(t, hraft.ValidateConfig(rc),
				"hraft must accept the derived config for operator input %+v", tc.cfg)
		})
	}
}
