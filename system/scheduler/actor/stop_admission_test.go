// SPDX-License-Identifier: MPL-2.0

package actor

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/payload"
	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/runtime"
)

// Shutdown is bounded, but never closes a process while OnStart still owns it.
// A successful late admission pairs OnStart with OnComplete without executing.
func TestStopDeadlineDuringLifecycleAdmission(t *testing.T) {
	var mu sync.Mutex
	var events []string
	record := func(e string) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	}
	entered, release := make(chan struct{}), make(chan struct{})
	lc := &testLifecycle{
		onStart: func(context.Context, pidapi.PID, process.Process) {
			close(entered)
			<-release
			record("start returned")
		},
		onComplete: func(context.Context, pidapi.PID, *runtime.Result) { record("complete") },
	}
	sched := newPreemptTestScheduler(1, lc)
	sched.Start()

	p := &closeCountingProcess{}
	submitted := make(chan error, 1)
	go func() {
		_, err := sched.Submit(context.Background(), pidapi.PID{UniqID: "admitting"}, p, "", nil)
		submitted <- err
	}()
	<-entered

	stopped := make(chan struct{})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		sched.Stop(ctx)
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(600 * time.Millisecond):
		t.Error("Stop ignored its deadline during OnStart")
	}
	if got := p.closed.Load(); got != 0 {
		t.Errorf("closed process while OnStart was running: %d", got)
	}
	mu.Lock()
	if len(events) != 0 {
		t.Errorf("completion before OnStart returned: %v", events)
	}
	mu.Unlock()
	close(release)

	if err := <-submitted; err != nil {
		t.Fatalf("submit: %v", err)
	}
	<-stopped

	mu.Lock()
	defer mu.Unlock()
	if want := []string{"start returned", "complete"}; !slices.Equal(events, want) {
		t.Fatalf("lifecycle events %v, want %v", events, want)
	}
	if got := p.closed.Load(); got != 1 {
		t.Fatalf("process closed %d times", got)
	}
}

type blockedAdmissionProcess struct {
	entered     chan struct{}
	release     chan struct{}
	cooperative bool
	closed      atomic.Int32
	stepped     atomic.Int32
}

func (p *blockedAdmissionProcess) Init(ctx context.Context, _ string, _ payload.Payloads) error {
	close(p.entered)
	if p.cooperative {
		<-ctx.Done()
		return ctx.Err()
	}
	<-p.release
	return nil
}

func (p *blockedAdmissionProcess) Step(_ []process.Event, out *process.StepOutput) error {
	p.stepped.Add(1)
	out.Done(nil)
	return nil
}

func (p *blockedAdmissionProcess) Close() { p.closed.Add(1) }

func TestStopDeadlineDuringInit(t *testing.T) {
	for _, cooperative := range []bool{true, false} {
		name := "ignores-cancel"
		if cooperative {
			name = "cooperative"
		}
		t.Run(name, func(t *testing.T) {
			var started, completed atomic.Int32
			sched := newPreemptTestScheduler(1, &testLifecycle{
				onStart:    func(context.Context, pidapi.PID, process.Process) { started.Add(1) },
				onComplete: func(context.Context, pidapi.PID, *runtime.Result) { completed.Add(1) },
			})
			sched.Start()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p := &blockedAdmissionProcess{entered: make(chan struct{}), release: make(chan struct{}), cooperative: cooperative}
			submitted := make(chan error, 1)
			go func() { _, err := sched.Submit(ctx, pidapi.PID{UniqID: name}, p, "", nil); submitted <- err }()
			<-p.entered
			stopped := make(chan struct{})
			go func() {
				stopCtx, stopCancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
				defer stopCancel()
				sched.Stop(stopCtx)
				close(stopped)
			}()
			select {
			case <-stopped:
			case <-time.After(500 * time.Millisecond):
				t.Error("Stop ignored its deadline during Init")
			}
			if got := p.closed.Load(); got != 0 {
				t.Errorf("scheduler closed caller-owned process: %d", got)
			}
			close(p.release)
			cancel() // Also unblocks the broken implementation on regression failure.
			if err := <-submitted; !errors.Is(err, context.Canceled) {
				t.Errorf("Submit = %v, want canceled admission", err)
			}
			<-stopped
			if started.Load() != 0 || completed.Load() != 0 || p.stepped.Load() != 0 {
				t.Errorf("canceled Init ran lifecycle or code: start=%d complete=%d step=%d", started.Load(), completed.Load(), p.stepped.Load())
			}
			if got := sched.processorCount.Load(); got != 0 {
				t.Errorf("leaked %d processors", got)
			}
			p.Close() // Failed Submit leaves cleanup with the caller.
		})
	}
}

func TestStopCancelsLifecycleAdmission(t *testing.T) {
	entered := make(chan struct{})
	var completed atomic.Int32
	sched := newPreemptTestScheduler(1, &testLifecycle{
		onStart:    func(ctx context.Context, _ pidapi.PID, _ process.Process) { close(entered); <-ctx.Done() },
		onComplete: func(context.Context, pidapi.PID, *runtime.Result) { completed.Add(1) },
	})
	sched.Start()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := &closeCountingProcess{}
	submitted := make(chan error, 1)
	go func() { _, err := sched.Submit(ctx, pidapi.PID{UniqID: "cancel-start"}, p, "", nil); submitted <- err }()
	<-entered
	stopped := make(chan struct{})
	go func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
		defer stopCancel()
		sched.Stop(stopCtx)
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(500 * time.Millisecond):
		t.Error("Stop did not cancel OnStart")
		cancel()
	}
	if err := <-submitted; err != nil {
		t.Fatalf("Submit: %v", err)
	}
	<-stopped
	if completed.Load() != 1 || p.closed.Load() != 1 {
		t.Fatalf("completion=%d close=%d; want one of each", completed.Load(), p.closed.Load())
	}
}

type rejectingAdmission struct {
	testLifecycle
	entered chan struct{}
	release chan struct{}
	err     error
}

func (l *rejectingAdmission) OnStart(context.Context, pidapi.PID, process.Process) error {
	close(l.entered)
	<-l.release
	return l.err
}

func TestLateRejectedAdmissionKeepsCallerOwnership(t *testing.T) {
	var completed atomic.Int32
	lc := &rejectingAdmission{
		entered: make(chan struct{}), release: make(chan struct{}), err: errors.New("rejected"),
		testLifecycle: testLifecycle{onComplete: func(context.Context, pidapi.PID, *runtime.Result) { completed.Add(1) }},
	}
	sched := newPreemptTestScheduler(1, &lc.testLifecycle)
	sched.lifecycle = lc
	sched.Start()
	p := &closeCountingProcess{}
	submitted := make(chan error, 1)
	go func() {
		_, err := sched.Submit(context.Background(), pidapi.PID{UniqID: "late-rejection"}, p, "", nil)
		submitted <- err
	}()
	<-lc.entered
	stopped := make(chan struct{})
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
		defer cancel()
		sched.Stop(ctx)
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(500 * time.Millisecond):
		t.Error("Stop ignored its deadline during rejected admission")
	}
	close(lc.release)
	if err := <-submitted; !errors.Is(err, lc.err) {
		t.Errorf("Submit=%v; want rejection", err)
	}
	<-stopped
	if p.closed.Load() != 0 || completed.Load() != 0 {
		t.Fatal("rejected process was completed or closed by scheduler")
	}
	if sched.processorCount.Load() != 0 {
		t.Fatal("rejected admission retained process count")
	}
	if _, ok := sched.byPID.Load("late-rejection"); ok {
		t.Fatal("rejected admission retained routing")
	}
	sched.admitMu.Lock()
	pending := len(sched.admissionCancels)
	sched.admitMu.Unlock()
	if pending != 0 {
		t.Fatal("rejected admission retained its cancel")
	}
	p.Close()
}
