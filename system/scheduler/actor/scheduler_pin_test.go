// SPDX-License-Identifier: MPL-2.0

package actor

import (
	"context"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/system/scheduler"
	"github.com/wippyai/runtime/system/scheduler/affinity"
)

func TestSchedulerThreadPinCompletes(t *testing.T) {
	var done atomic.Int32
	lc := &testLifecycle{
		onComplete: func(_ context.Context, _ pidapi.PID, _ *runtime.Result) { done.Add(1) },
	}

	reg := scheduler.NewRegistry()
	s := NewScheduler(reg, WithWorkers(3), WithThreadPin(affinity.Set{0}), WithLifecycle(lc))
	s.Start()

	const n = 100
	for i := 0; i < n; i++ {
		_, err := s.Submit(context.Background(), pidapi.PID{UniqID: "pin-" + strconv.Itoa(i)}, &SingleStepProcess{}, "", nil)
		if err != nil {
			t.Fatalf("submit %d: %v", i, err)
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	for done.Load() < n && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	s.Stop(context.Background())

	if got := done.Load(); got != n {
		t.Fatalf("expected %d completions under thread pinning, got %d", n, got)
	}
}
