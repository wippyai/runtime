// SPDX-License-Identifier: MPL-2.0

package actor

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/system/scheduler"
)

// quickProcess completes on its first step, driving the release/pool-reuse path.
// When closed is non-nil it is closed on teardown (race-safe), so a test can
// wait for the pid to be freed without polling the inspector.
type quickProcess struct {
	closed    chan struct{}
	closeOnce sync.Once
}

func (p *quickProcess) Init(context.Context, string, payload.Payloads) error { return nil }
func (p *quickProcess) Step(_ []process.Event, out *process.StepOutput) error {
	out.Done(nil)
	return nil
}
func (p *quickProcess) Send(*relay.Package) error { return nil }
func (p *quickProcess) Close() {
	if p.closed != nil {
		p.closeOnce.Do(func() { close(p.closed) })
	}
}

// outdatedRecorder is a long-lived process that records every OUTDATED event it
// receives and then parks. started is set on its first step (race-safe), so
// tests can wait for it without polling the inspector.
type outdatedRecorder struct {
	mu       sync.Mutex
	received [][]registry.ID
	started  atomic.Bool
}

func (p *outdatedRecorder) Init(context.Context, string, payload.Payloads) error { return nil }

func (p *outdatedRecorder) Step(events []process.Event, out *process.StepOutput) error {
	p.started.Store(true)
	for _, e := range events {
		if e.Type != process.EventMessage {
			continue
		}
		pkg, ok := e.Data.(*relay.Package)
		if !ok {
			continue
		}
		for _, msg := range pkg.Messages {
			if msg.Topic != topology.TopicEvents {
				continue
			}
			for _, pl := range msg.Payloads {
				if ev, ok := pl.Data().(*topology.OutdatedEvent); ok {
					p.mu.Lock()
					p.received = append(p.received, ev.Sources)
					p.mu.Unlock()
				}
			}
		}
	}
	out.Idle()
	return nil
}

func (p *outdatedRecorder) Send(*relay.Package) error { return nil }
func (p *outdatedRecorder) Close()                    {}

func (p *outdatedRecorder) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.received)
}

func (p *outdatedRecorder) lastSources() []registry.ID {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.received) == 0 {
		return nil
	}
	return p.received[len(p.received)-1]
}

func newOutdatedScheduler(t *testing.T, workers int) *Scheduler {
	t.Helper()
	reg := scheduler.NewRegistry()
	reg.Register(CmdComplete, CompleteHandler())
	reg.Register(CmdYield, YieldHandler())
	s := NewScheduler(reg, WithWorkers(workers))
	s.Start()
	return s
}

func submitWithSource(t *testing.T, s *Scheduler, uniq string, src registry.ID, proc process.Process) pidapi.PID {
	t.Helper()
	frameCtx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := runtime.SetFrameID(frameCtx, src); err != nil {
		t.Fatalf("SetFrameID: %v", err)
	}
	p := pidapi.PID{UniqID: uniq}
	if _, err := s.Submit(frameCtx, p, proc, "", nil); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	return p
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition not met before deadline")
}

// TestSendOutdatedTargetsAffected verifies SendOutdated delivers only to running
// instances whose source node is in the affected set.
func TestSendOutdatedTargetsAffected(t *testing.T) {
	s := newOutdatedScheduler(t, 2)
	defer testStopScheduler(s)

	worker := registry.NewID("app", "worker")
	other := registry.NewID("app", "other")
	lib := registry.NewID("app", "lib") // transitive dependent id, no instance

	recWorker := &outdatedRecorder{}
	recWorkerB := &outdatedRecorder{}
	recOther := &outdatedRecorder{}

	submitWithSource(t, s, "w1", worker, recWorker)
	submitWithSource(t, s, "w2", worker, recWorkerB)
	submitWithSource(t, s, "o1", other, recOther)

	waitFor(t, func() bool { return len(s.ListProcesses()) == 3 })

	affected := map[registry.ID]bool{worker: true, lib: true}
	s.SendOutdated(affected)

	waitFor(t, func() bool { return recWorker.count() == 1 && recWorkerB.count() == 1 })

	// Unaffected instance is never targeted.
	time.Sleep(20 * time.Millisecond)
	if recOther.count() != 0 {
		t.Fatalf("unaffected instance received %d OUTDATED events", recOther.count())
	}

	// Sources carry the sorted affected set, identical for every matched instance.
	want := []registry.ID{lib, worker} // "app:lib" < "app:worker"
	if got := recWorker.lastSources(); !equalIDs(got, want) {
		t.Fatalf("worker sources = %v, want %v", got, want)
	}
	if got := recWorkerB.lastSources(); !equalIDs(got, want) {
		t.Fatalf("worker-b sources = %v, want %v", got, want)
	}
}

// TestSendOutdatedEmptySetNoOp verifies an empty affected set targets nothing.
func TestSendOutdatedEmptySetNoOp(t *testing.T) {
	s := newOutdatedScheduler(t, 2)
	defer testStopScheduler(s)

	rec := &outdatedRecorder{}
	submitWithSource(t, s, "w1", registry.NewID("app", "worker"), rec)
	waitFor(t, func() bool { return len(s.ListProcesses()) == 1 })

	s.SendOutdated(nil)
	s.SendOutdated(map[registry.ID]bool{})

	time.Sleep(20 * time.Millisecond)
	if rec.count() != 0 {
		t.Fatalf("empty affected set delivered %d events", rec.count())
	}
}

// TestSendOutdatedDeterministicTargeting verifies targeting does not depend on
// scan order: repeated invocations with the same set hit the same instances.
func TestSendOutdatedDeterministicTargeting(t *testing.T) {
	s := newOutdatedScheduler(t, 4)
	defer testStopScheduler(s)

	a := registry.NewID("app", "a")
	b := registry.NewID("app", "b")

	recA := &outdatedRecorder{}
	recB := &outdatedRecorder{}
	submitWithSource(t, s, "a1", a, recA)
	submitWithSource(t, s, "b1", b, recB)
	waitFor(t, func() bool { return len(s.ListProcesses()) == 2 })

	for i := 0; i < 5; i++ {
		s.SendOutdated(map[registry.ID]bool{a: true})
	}

	waitFor(t, func() bool { return recA.count() == 5 })
	time.Sleep(20 * time.Millisecond)
	if recB.count() != 0 {
		t.Fatalf("instance b targeted despite not being in the affected set (%d)", recB.count())
	}
}

// TestDeliverToProcGenerationGuard proves the generation guard SendOutdated
// relies on: a delivery under a generation that does not match the processor's
// live queue generation is rejected, so a stale snapshot (from a released/reused
// slot) never reaches the current occupant.
func TestDeliverToProcGenerationGuard(t *testing.T) {
	s := newOutdatedScheduler(t, 1)
	defer testStopScheduler(s)

	x := registry.NewID("app", "x")
	rec := &outdatedRecorder{}
	pid := submitWithSource(t, s, "p", x, rec)
	waitFor(t, func() bool { return len(s.ListProcesses()) == 1 })

	v, _ := s.byPID.Load(pid.String())
	proc := v.(*Processor)

	// A generation that cannot match the live queue (as a stale snapshot would).
	if s.deliverToProc(proc, proc.sig.Load().gen+1000, topology.OutdatedPackage(pid, []registry.ID{x})) {
		t.Fatal("delivery under a non-matching generation must be rejected")
	}
	time.Sleep(20 * time.Millisecond)
	if rec.count() != 0 {
		t.Fatalf("stale-generation delivery reached the process (%d)", rec.count())
	}

	// The live generation delivers.
	if !s.deliverToProc(proc, proc.queue.Generation(), topology.OutdatedPackage(pid, []registry.ID{x})) {
		t.Fatal("delivery under the live generation must succeed")
	}
	waitFor(t, func() bool { return rec.count() == 1 })
}

// TestSendOutdatedPidReuseNoMisdelivery submits process A on source X with a
// caller-supplied pid, lets it complete (freeing the pid), then submits B on
// source Y reusing the same pid. Invalidating X must never reach B; invalidating
// Y does.
func TestSendOutdatedPidReuseNoMisdelivery(t *testing.T) {
	s := newOutdatedScheduler(t, 2)
	defer testStopScheduler(s)

	x := registry.NewID("app", "x")
	y := registry.NewID("app", "y")
	const reusedPID = "reused-pid"

	// A: source X, completes immediately and releases the pid/slot. Wait on its
	// Close (fires after byPID delete) rather than polling the inspector.
	procA := &quickProcess{closed: make(chan struct{})}
	submitWithSource(t, s, reusedPID, x, procA)
	<-procA.closed

	// B: source Y, SAME pid, long-lived recorder.
	recB := &outdatedRecorder{}
	submitWithSource(t, s, reusedPID, y, recB)
	waitFor(t, func() bool { return recB.started.Load() })

	// Invalidating A's source must not reach B on the recycled pid.
	s.SendOutdated(map[registry.ID]bool{x: true})
	time.Sleep(20 * time.Millisecond)
	if recB.count() != 0 {
		t.Fatalf("recycled pid mis-delivered source X to B (%d events)", recB.count())
	}

	// Invalidating B's own source reaches it.
	s.SendOutdated(map[registry.ID]bool{y: true})
	waitFor(t, func() bool { return recB.count() == 1 })
}

// TestSendOutdatedFollowsCrossSourceUpgrade upgrades a process to a NEW source
// and verifies future invalidations classify it by the new source: invalidating
// the new source delivers OUTDATED; invalidating the old source does not.
func TestSendOutdatedFollowsCrossSourceUpgrade(t *testing.T) {
	reg := scheduler.NewRegistry()
	reg.Register(CmdComplete, CompleteHandler())
	reg.Register(CmdYield, YieldHandler())
	s := NewScheduler(reg, WithWorkers(2))
	s.Start()
	defer testStopScheduler(s)

	oldSrc := registry.NewID("app", "old")
	newSrc := registry.NewID("app", "new")
	rec := &outdatedRecorder{}

	ctx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	process.WithFactory(ctx, &mockFactory{
		createFunc: func(registry.ID) (process.Process, *process.Meta, error) {
			return rec, &process.Meta{Method: "main"}, nil
		},
	})

	frameCtx, _ := ctxapi.OpenFrameContext(ctx)
	if err := runtime.SetFrameID(frameCtx, oldSrc); err != nil {
		t.Fatalf("SetFrameID: %v", err)
	}
	upgrader := &UpgradeProcess{upgradeReq: &process.UpgradeRequest{Source: newSrc}}
	upgraderPID := pidapi.PID{UniqID: "upgrader"}
	if _, err := s.Submit(frameCtx, upgraderPID, upgrader, "", nil); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// Wait for the upgraded recorder to run (race-safe), which happens-after the
	// upgrade republished the snapshot, then assert the snapshot carries the new
	// source via the atomic accessor (not the inspector, which races the swap).
	waitFor(t, func() bool { return rec.started.Load() })
	v, ok := s.byPID.Load(upgraderPID.String())
	if !ok {
		t.Fatal("upgraded process missing from byPID")
	}
	ref := v.(*Processor).sig.Load()
	if ref == nil || !ref.source.Equal(newSrc) {
		t.Fatalf("post-upgrade snapshot source = %v, want %v", ref, newSrc)
	}

	// Invalidating the pre-upgrade source must not reach it.
	s.SendOutdated(map[registry.ID]bool{oldSrc: true})
	time.Sleep(20 * time.Millisecond)
	if rec.count() != 0 {
		t.Fatalf("cross-upgraded process wrongly notified for old source (%d)", rec.count())
	}

	// Invalidating the new source delivers.
	s.SendOutdated(map[registry.ID]bool{newSrc: true})
	waitFor(t, func() bool { return rec.count() == 1 })
}

// TestSendOutdatedConcurrentExitReuse drives rapid process completion and
// pool reuse while SendOutdated scans concurrently. Under -race this proves the
// scan reads only the atomically published snapshot, never a Processor being
// released or reused.
func TestSendOutdatedConcurrentExitReuse(t *testing.T) {
	s := newOutdatedScheduler(t, 4)
	defer testStopScheduler(s)

	src := registry.NewID("app", "churn")
	affected := map[registry.ID]bool{src: true}

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Submitter: short-lived processes that complete and get reused.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			frameCtx, _ := ctxapi.OpenFrameContext(context.Background())
			_ = runtime.SetFrameID(frameCtx, src)
			p := pidapi.PID{UniqID: "churn-" + strconv.Itoa(i)}
			_, _ = s.Submit(frameCtx, p, &quickProcess{}, "", nil)
		}
	}()

	// Scanner: concurrent SendOutdated against the churning table.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			s.SendOutdated(affected)
		}
	}()

	time.Sleep(300 * time.Millisecond)
	close(stop)
	wg.Wait()
}

func equalIDs(a, b []registry.ID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !a[i].Equal(b[i]) {
			return false
		}
	}
	return true
}
