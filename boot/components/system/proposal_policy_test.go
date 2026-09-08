// SPDX-License-Identifier: MPL-2.0

package system

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
	raftapi "github.com/wippyai/runtime/api/cluster/raft"
)

func TestPendingApplyBudgets(t *testing.T) {
	cfg := boot.NewConfig(boot.WithSection(ClusterName, map[string]any{}))
	count, bytes, err := loadPendingApplyLimits(cfg.Sub(ClusterName))
	require.NoError(t, err)
	require.Equal(t, raftapi.DefaultMaxPendingApplies, count)
	require.Equal(t, raftapi.DefaultMaxPendingApplyBytes, bytes)
	cfg = boot.NewConfig(boot.WithSection(ClusterName, map[string]any{"raft.max_pending_applies": 3, "raft.max_pending_apply_bytes": "4096"}))
	count, bytes, err = loadPendingApplyLimits(cfg.Sub(ClusterName))
	require.NoError(t, err)
	require.Equal(t, 3, count)
	require.Equal(t, 4096, bytes)
	for _, key := range []string{"raft.max_pending_applies", "raft.max_pending_apply_bytes"} {
		for _, value := range []any{0, -1, 1.5, true, "invalid"} {
			cfg := boot.NewConfig(boot.WithSection(ClusterName, map[string]any{key: value}))
			_, _, err := loadPendingApplyLimits(cfg.Sub(ClusterName))
			require.ErrorContains(t, err, key)
		}
	}
}
