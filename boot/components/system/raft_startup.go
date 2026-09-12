// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"
	"fmt"
	"time"
)

// waitRaftLeader observes the actual leader hint, including on followers. The
// hashicorp leadership channel describes this node's own leadership and already
// belongs to the telemetry loop; it is not a broadcast readiness notification.
// A known leader is only a routing prerequisite. Participant startup still
// requires a fresh authority barrier and exclusion snapshot before admission.
func waitRaftLeader(ctx context.Context, known func() bool, timeout, interval time.Duration) error {
	if timeout <= 0 || interval <= 0 {
		return fmt.Errorf("raft startup timeout and poll interval must be positive")
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := waitCtx.Err(); err != nil {
		return err
	}
	if known() {
		return nil
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-waitCtx.Done():
			return waitCtx.Err()
		case <-ticker.C:
			if err := waitCtx.Err(); err != nil {
				return err
			}
			if known() {
				return nil
			}
		}
	}
}
