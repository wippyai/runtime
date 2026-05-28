// SPDX-License-Identifier: MPL-2.0

package raft

import (
	"errors"
	"runtime"
	"testing"
	"time"

	raftapi "github.com/wippyai/runtime/api/cluster/raft"
)

// hangingFuture is a stub that blocks in Error() forever, simulating a
// hashicorp/raft leadership-transfer future under a network partition.
type hangingFuture struct{}

func (hangingFuture) Error() error {
	select {} // block forever
}

// TestAwaitFutureWithTimeout_ReturnsErrTimeout verifies that
// awaitFutureWithTimeout returns ErrTimeout within the deadline when the
// underlying future never resolves.
func TestAwaitFutureWithTimeout_ReturnsErrTimeout(t *testing.T) {
	got := awaitFutureWithTimeout(hangingFuture{}, 30*time.Millisecond)
	if !errors.Is(got, raftapi.ErrTimeout) {
		t.Fatalf("expected ErrTimeout, got %v", got)
	}
}

// TestAwaitFutureWithTimeout_BoundedGoroutineLeak verifies that a timed-out
// transfer pins at most one goroutine (the one blocked inside f.Error()).
// hraft.Future has no cancellation API, so a single pinned goroutine is the
// expected and documented behavior; the goroutine exits when hraft shuts
// down and the future resolves.  This test guards against regressions that
// would create additional goroutines per call.
func TestAwaitFutureWithTimeout_BoundedGoroutineLeak(t *testing.T) {
	// Settle background goroutines from the test runtime before sampling.
	runtime.Gosched()
	time.Sleep(20 * time.Millisecond)

	before := runtime.NumGoroutine()

	got := awaitFutureWithTimeout(hangingFuture{}, 30*time.Millisecond)
	if !errors.Is(got, raftapi.ErrTimeout) {
		t.Fatalf("expected ErrTimeout, got %v", got)
	}

	// Allow any transient goroutines (e.g. time.After internals) to settle.
	time.Sleep(50 * time.Millisecond)

	leaked := runtime.NumGoroutine() - before
	// Exactly one goroutine is expected to be pinned inside hangingFuture.Error().
	// More than one indicates a regression.
	if leaked > 1 {
		t.Fatalf("expected at most 1 goroutine pinned by stuck future, got %d", leaked)
	}
}
