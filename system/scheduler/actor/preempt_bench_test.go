// SPDX-License-Identifier: MPL-2.0

package actor

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/wippyai/runtime/api/payload"
	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

var benchSink atomic.Uint64

// sliceProcess burns a fixed amount of work per step and reports preemption
// until the shared slice budget is used up.
type sliceProcess struct {
	slices *atomic.Int64
	work   int
}

func (*sliceProcess) Init(context.Context, string, payload.Payloads) error { return nil }
func (p *sliceProcess) EnablePreemption()                                  {}
func (p *sliceProcess) Step(_ []process.Event, out *process.StepOutput) error {
	if p.slices.Add(-1) < 0 {
		out.Done(nil)
		return nil
	}
	var acc uint64
	for i := 0; i < p.work; i++ {
		acc += uint64(i) * 2654435761
	}
	benchSink.Add(acc & 1)
	out.Preempt()
	return nil
}
func (*sliceProcess) Send(*relay.Package) error { return nil }
func (*sliceProcess) Close()                    {}

// BenchmarkPreemptedSlices measures scheduler cost per preempted step with
// one or two spinning actors per worker; ns/op is wall time per slice.
func BenchmarkPreemptedSlices(b *testing.B) {
	for _, workers := range []int{1, 2, 4, 8, 16} {
		for _, perWorker := range []int{1, 2} {
			b.Run(fmt.Sprintf("workers=%d/actors=%dx", workers, perWorker), func(b *testing.B) {
				const work = 500
				var wg sync.WaitGroup
				lc := &testLifecycle{
					onComplete: func(context.Context, pidapi.PID, *runtime.Result) { wg.Done() },
				}
				sched := newPreemptTestScheduler(workers, lc)
				sched.Start()
				defer testStopScheduler(sched)

				var slices atomic.Int64
				slices.Store(int64(b.N))
				actors := workers * perWorker
				wg.Add(actors)
				b.ResetTimer()
				for i := 0; i < actors; i++ {
					p := &sliceProcess{slices: &slices, work: work}
					if _, err := sched.Submit(context.Background(), pidapi.PID{UniqID: fmt.Sprintf("spin-%d", i)}, p, "", nil); err != nil {
						b.Fatal(err)
					}
				}
				wg.Wait()
			})
		}
	}
}

// BenchmarkPreemptedRequeue drives workers by hand, one goroutine per worker
// and without parking, to measure the dispatch and requeue cost of a
// preempted step; ns/op is per slice across all workers.
func BenchmarkPreemptedRequeue(b *testing.B) {
	for _, workers := range []int{1, 4, 16} {
		for _, perWorker := range []int{1, 2} {
			b.Run(fmt.Sprintf("workers=%d/actors=%dx", workers, perWorker), func(b *testing.B) {
				sched := newPreemptTestScheduler(workers, &testLifecycle{})
				var slices atomic.Int64
				slices.Store(int64(b.N) + int64(workers*perWorker))
				for i := 0; i < workers*perWorker; i++ {
					p := &sliceProcess{slices: &slices}
					if _, err := sched.Submit(context.Background(), pidapi.PID{UniqID: fmt.Sprintf("spin-%d", i)}, p, "", nil); err != nil {
						b.Fatal(err)
					}
				}
				snapshot := sched.workerSnapshot()
				var next atomic.Int32
				b.ResetTimer()
				var wg sync.WaitGroup
				for g := 0; g < workers; g++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						w := snapshot[int(next.Add(1))-1]
						for slices.Load() > 0 {
							if proc := w.findWork(); proc != nil {
								w.executeOne(proc)
							}
						}
					}()
				}
				wg.Wait()
			})
		}
	}
}
