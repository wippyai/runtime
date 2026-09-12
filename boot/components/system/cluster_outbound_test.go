// SPDX-License-Identifier: MPL-2.0
package system

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/cluster/internode"
)

func TestClusterOutboundConfiguration(t *testing.T) {
	defaults := internode.DefaultManagerConfig()
	cfg := boot.NewConfig(boot.WithSection("cluster", map[string]any{
		"internode.outbound.peer_entries":  32,
		"internode.outbound.batch_bytes":   4096,
		"internode.outbound.peer_bytes":    uint64(1) << 32,
		"internode.outbound.total_entries": "1000",
		"internode.outbound.total_bytes":   "8589934592",
	})).Sub("cluster")
	require.NoError(t, configureClusterOutbound(cfg, &defaults))
	require.Equal(t, 32, defaults.OutboundQueueSize)
	require.Equal(t, uint64(4096), defaults.DrainBatchBytes)
	require.Equal(t, uint64(1)<<32, defaults.OutboundPeerBytes)
	require.Equal(t, uint64(1000), defaults.OutboundTotalEntries)
	require.Equal(t, uint64(1)<<33, defaults.OutboundTotalBytes)
}

func TestClusterOutboundRejectsInvalidConfigurationWithoutPartialMutation(t *testing.T) {
	for _, value := range []any{0, -1, "-8", "1.5", true, "18446744073709551616", nil} {
		candidate := internode.DefaultManagerConfig()
		original := candidate.OutboundPeerBytes
		cfg := boot.NewConfig(boot.WithSection("cluster", map[string]any{
			"internode.outbound.peer_bytes":  32,
			"internode.outbound.total_bytes": value,
		})).Sub("cluster")
		err := configureClusterOutbound(cfg, &candidate)
		require.ErrorContains(t, err, "cluster.internode.outbound.total_bytes")
		require.Equal(t, original, candidate.OutboundPeerBytes)
	}
	candidate := internode.DefaultManagerConfig()
	cfg := boot.NewConfig().Sub("cluster")
	require.NoError(t, configureClusterOutbound(cfg, &candidate))
	require.Equal(t, internode.DefaultManagerConfig().OutboundTotalBytes, candidate.OutboundTotalBytes)
}

func TestClusterOutboundControlReserves(t *testing.T) {
	keys := []string{"peer_entries", "peer_bytes", "total_entries", "total_bytes"}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			path := "internode.outbound.control." + key
			for _, value := range []any{uint64(0), uint64(1), "2"} {
				candidate := internode.DefaultManagerConfig()
				cfg := boot.NewConfig(boot.WithSection("cluster", map[string]any{path: value})).Sub("cluster")
				require.NoError(t, configureClusterOutbound(cfg, &candidate))
				got := map[string]uint64{"peer_entries": candidate.OutboundControlPeerEntries, "peer_bytes": candidate.OutboundControlPeerBytes, "total_entries": candidate.OutboundControlTotalEntries, "total_bytes": candidate.OutboundControlTotalBytes}
				require.Equal(t, fmt.Sprint(value), fmt.Sprint(got[key]))
			}
			for _, value := range []any{-1, "18446744073709551615", true, "1.5"} {
				candidate := internode.DefaultManagerConfig()
				cfg := boot.NewConfig(boot.WithSection("cluster", map[string]any{path: value, "internode.outbound.peer_bytes": 32 << 20})).Sub("cluster")
				require.ErrorContains(t, configureClusterOutbound(cfg, &candidate), path)
				require.Equal(t, internode.DefaultManagerConfig().OutboundPeerBytes, candidate.OutboundPeerBytes)
			}
		})
	}
}
