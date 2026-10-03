// SPDX-License-Identifier: MPL-2.0

package actor

import (
	"sync"
	"testing"

	"github.com/wippyai/runtime/api/process"
)

// A wake bound to an earlier incarnation of a processor slot never changes the
// state of the incarnation that reuses the slot, whichever way the reuse
// interleaves with the wake.
func TestStaleWakeDoesNotTransitionReusedSlot(t *testing.T) {
	s := NewScheduler(nil)
	proc := &Processor{queue: process.NewEventQueue()}
	s.byQueue.Store(proc.queue, proc)

	const rounds = 20000
	for i := 0; i < rounds; i++ {
		oldGen := uint64(2*i + 1)
		proc.gen.Store(oldGen)
		proc.state.Store(int32(StateBlocked))

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.WakeProcessor(proc.queue, oldGen)
		}()
		// The slot is reused: the next incarnation publishes its generation and
		// blocks.
		proc.setGeneration(oldGen + 1)
		proc.state.Store(int32(StateBlocked))
		wg.Wait()

		if got := ProcessState(proc.state.Load()) & stateMask; got != StateBlocked {
			t.Fatalf("round %d: stale wake moved the reused slot to %s", i, StateName(got))
		}
		for s.global.Pop() != nil {
		}
	}
}

func TestStaleWakeLeavesRunningSlotUnflagged(t *testing.T) {
	proc := &Processor{queue: process.NewEventQueue()}
	proc.setGeneration(2)
	proc.state.Store(int32(StateRunning))

	if proc.wake(1) {
		t.Fatal("a stale wake must not schedule the processor")
	}
	if got := ProcessState(proc.state.Load()); got != StateRunning {
		t.Fatalf("stale wake changed the state to %d", got)
	}
	if proc.wake(2); ProcessState(proc.state.Load()) != StateRunning|wakeupFlag {
		t.Fatalf("a current wake flags the running processor, state %d", proc.state.Load())
	}
}
