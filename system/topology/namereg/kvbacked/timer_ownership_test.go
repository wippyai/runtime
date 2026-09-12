// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDelayedTimerCallbackPreservesReplacement(t *testing.T) {
	r := newStrongReg(t, nil, 0, nil)
	st := r.strong
	deadline := time.Now().Add(time.Hour).UnixNano()
	st.armTimer("name", deadline)
	st.mu.Lock()
	old := st.timers["name"]
	st.mu.Unlock()
	// Suppress real scheduling, then manually deliver the old callback after
	// replacement to exercise the Stop(false)/already-fired interleaving.
	old.timer.Stop()
	st.stopTimer("name")
	st.armTimer("name", deadline)
	st.mu.Lock()
	replacement := st.timers["name"]
	st.mu.Unlock()
	defer st.stopTimer("name")
	st.timerFired("name", old)
	st.mu.Lock()
	current := st.timers["name"]
	st.mu.Unlock()
	if current != replacement {
		t.Fatal("old timer callback removed its replacement")
	}
	select {
	case <-old.done:
	default:
		t.Fatal("old callback did not release its ownership")
	}
}

func TestTimerShutdownJoinsExpiredCallbackAndCanRetry(t *testing.T) {
	r := newStrongReg(t, nil, 0, nil)
	engine := &heldFirstSnapshot{Engine: r.engine, captured: make(chan struct{}), release: make(chan struct{})}
	r.engine = engine
	r.strong.armTimer("name", time.Now().UnixNano())
	<-engine.captured
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.strong.stopTimers(ctx); !errors.Is(err, context.Canceled) {
		close(engine.release)
		t.Fatalf("shutdown ignored outstanding expired callback: %v", err)
	}
	// A sealed owner cannot schedule additional work while joining.
	r.strong.armTimer("other", time.Now().Add(time.Hour).UnixNano())
	r.strong.mu.Lock()
	scheduled, outstanding := len(r.strong.timers), len(r.strong.timerWork)
	r.strong.mu.Unlock()
	if scheduled != 0 || outstanding != 1 {
		close(engine.release)
		t.Fatalf("scheduled=%d outstanding=%d", scheduled, outstanding)
	}
	close(engine.release)
	if err := r.strong.stopTimers(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.strong.mu.Lock()
	outstanding = len(r.strong.timerWork)
	r.strong.mu.Unlock()
	if outstanding != 0 {
		t.Fatal("shutdown returned with callback work retained")
	}
}

func TestStopReconcilerJoinsExpiredTimerBeforeCompletion(t *testing.T) {
	r := newStrongReg(t, nil, 0, nil)
	engine := &heldFirstSnapshot{Engine: r.engine, captured: make(chan struct{}), release: make(chan struct{})}
	r.engine = engine
	if err := r.StartReconciler(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.strong.armTimer("name", time.Now().UnixNano())
	<-engine.captured
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.StopReconciler(ctx); !errors.Is(err, context.Canceled) {
		close(engine.release)
		_ = r.StopReconciler(context.Background())
		t.Fatalf("stop completed with a blocked callback: %v", err)
	}
	if r.NameReady() {
		close(engine.release)
		_ = r.StopReconciler(context.Background())
		t.Fatal("stopping owner still admits names")
	}
	close(engine.release)
	if err := r.StopReconciler(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.strong.mu.Lock()
	work := len(r.strong.timerWork)
	r.strong.mu.Unlock()
	if work != 0 {
		t.Fatal("joined reconciler retains timer work")
	}
	if err := r.StartReconciler(context.Background()); err == nil {
		t.Fatal("stopped owner restarted")
	}
}
