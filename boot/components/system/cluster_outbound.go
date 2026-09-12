// SPDX-License-Identifier: MPL-2.0
package system

import (
	"fmt"
	"strconv"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/cluster/internode"
)

// configureClusterOutbound parses byte counts without converting through int,
// so the aggregate budget has identical meaning on 32- and 64-bit hosts.
// Absent keys preserve manager defaults; explicit zero is a configuration error.
func configureClusterOutbound(cfg boot.Config, manager *internode.ManagerConfig) error {
	candidate := *manager
	fields := []struct {
		key   string
		value *uint64
	}{
		{"internode.outbound.batch_bytes", &candidate.DrainBatchBytes},
		{"internode.outbound.peer_bytes", &candidate.OutboundPeerBytes},
		{"internode.outbound.total_bytes", &candidate.OutboundTotalBytes},
		{"internode.outbound.total_entries", &candidate.OutboundTotalEntries},
	}
	peerEntries := uint64(candidate.OutboundQueueSize)
	fields = append(fields, struct {
		key   string
		value *uint64
	}{"internode.outbound.peer_entries", &peerEntries})
	for _, field := range fields {
		raw, found := cfg.Get(field.key)
		if !found {
			continue
		}
		value, err := strconv.ParseUint(fmt.Sprint(raw), 10, 64)
		if err != nil || value == 0 {
			return fmt.Errorf("cluster.%s requires a positive decimal integer", field.key)
		}
		*field.value = value
	}
	if peerEntries > uint64(^uint(0)>>1) {
		return fmt.Errorf("cluster.internode.outbound.peer_entries exceeds platform capacity")
	}
	candidate.OutboundQueueSize = int(peerEntries)
	reserves := []struct {
		key   string
		value *uint64
		limit uint64
	}{
		{"internode.outbound.control.peer_entries", &candidate.OutboundControlPeerEntries, peerEntries},
		{"internode.outbound.control.peer_bytes", &candidate.OutboundControlPeerBytes, candidate.OutboundPeerBytes},
		{"internode.outbound.control.total_entries", &candidate.OutboundControlTotalEntries, candidate.OutboundTotalEntries},
		{"internode.outbound.control.total_bytes", &candidate.OutboundControlTotalBytes, candidate.OutboundTotalBytes},
	}
	for _, field := range reserves {
		if raw, found := cfg.Get(field.key); found {
			value, err := strconv.ParseUint(fmt.Sprint(raw), 10, 64)
			if err != nil {
				return fmt.Errorf("cluster.%s requires a nonnegative decimal integer", field.key)
			}
			*field.value = value
		}
		if *field.value >= field.limit {
			return fmt.Errorf("cluster.%s must leave ordinary capacity within its total", field.key)
		}
	}
	*manager = candidate
	return nil
}
