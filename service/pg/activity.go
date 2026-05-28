// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"sync/atomic"
	"time"
)

// activityTracker is a single atomic timestamp updated on every PG
// broadcast send/receive. Used by the runtime liveness probe to
// distinguish "alive but stuck on the wrong side of partition" from
// "alive and doing work" — under chaos network-partition the previous
// behavior was a pod that stayed Ready while making no real progress
// (49 MiB while peers held 474 MiB; no broadcasts emitted, none
// received). Without an activity-based liveness signal kubelet had no
// way to tell.
//
// Stored as a Unix nanosecond count so it fits in a single atomic.
// Calls are O(1) and lock-free.
type activityTracker struct {
	lastNanos atomic.Int64
}

func newActivityTracker() *activityTracker {
	t := &activityTracker{}
	t.lastNanos.Store(time.Now().UnixNano())
	return t
}

// Touch is called on every PG broadcast TX or RX. Cheap; safe to call
// from the hot path of the event loop.
func (t *activityTracker) Touch() {
	t.lastNanos.Store(time.Now().UnixNano())
}

// Since returns how long it has been since the last Touch. Zero
// indicates "never touched" (only possible at process start before
// the first broadcast); the constructor seeds with time.Now() so a
// fresh tracker reports `Since() ~= 0` rather than an unbounded
// duration.
func (t *activityTracker) Since() time.Duration {
	last := t.lastNanos.Load()
	if last == 0 {
		return 0
	}
	return time.Since(time.Unix(0, last))
}
