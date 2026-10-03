// SPDX-License-Identifier: MPL-2.0

package actor

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/runtime"
)

// Stop does not complete a processor whose admission is still in progress:
// the processor finishes admission, is published, and stops like any other.
func TestStopWaitsForProcessorsBeingAdmitted(t *testing.T) {
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
	// Without a wait for admission Stop returns once its deadline passes.
	select {
	case <-stopped:
	case <-time.After(600 * time.Millisecond):
	}
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
