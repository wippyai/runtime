// SPDX-License-Identifier: MPL-2.0

package system

import (
	"fmt"
	"strconv"

	"github.com/wippyai/runtime/api/boot"
	raftapi "github.com/wippyai/runtime/api/cluster/raft"
)

func loadPendingApplyLimits(cfg boot.Config) (int, int, error) {
	count, bytes := raftapi.DefaultMaxPendingApplies, raftapi.DefaultMaxPendingApplyBytes
	for _, field := range []struct {
		target *int
		name   string
	}{
		{&count, "max_pending_applies"}, {&bytes, "max_pending_apply_bytes"},
	} {
		key := "raft." + field.name
		if value, exists := cfg.Get(key); exists {
			parsed, err := strconv.Atoi(fmt.Sprint(value))
			if err != nil || parsed <= 0 {
				return 0, 0, fmt.Errorf("cluster.%s must be a positive integer", key)
			}
			*field.target = parsed
		}
	}
	return count, bytes, nil
}
