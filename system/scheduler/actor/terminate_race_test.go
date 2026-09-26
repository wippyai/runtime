// SPDX-License-Identifier: MPL-2.0

package actor

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/runtime"
)

// Terminate may look a process up while the process completes and its pooled
// slot is recycled. It must act on that process only: never race the slot's
// release and never terminate the process that reuses the slot.
func TestTerminateRacesCompletionWithoutTouchingReusedSlot(t *testing.T) {
	survivorDone := make(chan *runtime.Result, 1)
	lc := &testLifecycle{onComplete: func(_ context.Context, p pidapi.PID, res *runtime.Result) {
		if p.UniqID == "survivor" {
			survivorDone <- res
		}
	}}
	sched := newTestSchedulerWithLifecycle(4, lc)
	sched.Start()
	defer testStopScheduler(sched)

	const rounds = 500
	var wg sync.WaitGroup
	for i := range rounds {
		pid := pidapi.PID{UniqID: fmt.Sprintf("short-%d", i)}
		_, err := sched.Submit(context.Background(), pid, &SingleStepProcess{}, "", nil)
		require.NoError(t, err)
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = sched.Terminate(pid)
		}()
	}
	wg.Wait()

	// A process started after the churn keeps running when a finished PID
	// that may have held its slot is terminated.
	survivor := pidapi.PID{UniqID: "survivor"}
	_, err := sched.Submit(context.Background(), survivor, &SleepProcess{duration: 200 * time.Millisecond}, "", nil)
	require.NoError(t, err)
	for i := range rounds {
		_ = sched.Terminate(pidapi.PID{UniqID: fmt.Sprintf("short-%d", i)})
	}
	select {
	case res := <-survivorDone:
		require.NoError(t, res.Error, "survivor was terminated through a stale PID")
	case <-time.After(5 * time.Second):
		t.Fatal("survivor did not complete")
	}
}
