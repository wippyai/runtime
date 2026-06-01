// SPDX-License-Identifier: MPL-2.0

package clock

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	clockapi "github.com/wippyai/runtime/api/clock"
	"github.com/wippyai/runtime/api/payload"
)

func waitForClockPackages(t *testing.T, node *capturingNode, min int) []*relayPackageSnapshot {
	t.Helper()
	deadline := time.After(time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		pkgs := node.snapshot()
		if len(pkgs) >= min {
			out := make([]*relayPackageSnapshot, 0, len(pkgs))
			for _, pkg := range pkgs {
				if len(pkg.Messages) == 0 {
					continue
				}
				out = append(out, &relayPackageSnapshot{payloads: pkg.Messages[0].Payloads})
			}
			return out
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %d packages, got %d", min, len(pkgs))
		case <-tick.C:
		}
	}
}

type relayPackageSnapshot struct {
	payloads payload.Payloads
}

func startMatrixTimer(ctx context.Context, t *testing.T, d *Dispatcher, duration time.Duration, chID uint64, build clockapi.FireBuilder, genRef *atomic.Uint64) clockapi.TimerStartResult {
	t.Helper()
	rcv := newMockReceiver()
	if err := d.handleTimerStart(ctx, clockapi.TimerStartCmd{
		PID:      samplePID("timer"),
		Topic:    "timer-topic",
		Duration: duration,
		ChID:     chID,
		Epoch:    7,
		GenRef:   genRef,
		Build:    build,
	}, 1, rcv); err != nil {
		t.Fatal(err)
	}
	rcv.await(t, time.Second)
	data, err := rcv.snapshot()
	if err != nil {
		t.Fatalf("timer start failed: %v", err)
	}
	result, ok := data.(clockapi.TimerStartResult)
	if !ok {
		t.Fatalf("expected TimerStartResult, got %T", data)
	}
	return result
}

func assertClockTimerMaps(t *testing.T, d *Dispatcher, timers, reverse int) {
	t.Helper()
	if got := d.TimerCount(); got != timers {
		t.Fatalf("TimerCount=%d, want %d", got, timers)
	}
	gotTimers, _ := d.ReverseMapSize()
	if gotTimers != reverse {
		t.Fatalf("timer reverse map=%d, want %d", gotTimers, reverse)
	}
}

// TestClockSubscriptionLifecycleProofMatrix covers the dispatcher-side
// lifecycle guarantees needed by subscription-owned cleanup.
func TestClockSubscriptionLifecycleProofMatrix(t *testing.T) {
	// case1 fire+read: a fired one-shot timer self-retires and emits a
	// terminal payload after the value.
	t.Run("case1 fire+read", func(t *testing.T) {
		d, node, ctx, cancel := newDispatcherWithCtx(t)
		defer stopDispatcher(t, d, cancel)

		startMatrixTimer(ctx, t, d, 5*time.Millisecond, 1, func(at time.Time, gen uint64) payload.Payload {
			return payload.NewPayload(at.UnixNano(), payload.Golang)
		}, nil)

		pkgs := waitForClockPackages(t, node, 1)
		last := pkgs[len(pkgs)-1]
		if len(last.payloads) < 2 || !payload.IsTerminal(last.payloads[len(last.payloads)-1]) {
			t.Fatalf("timer fire payloads = %#v, want value plus terminal", last.payloads)
		}
		assertClockTimerMaps(t, d, 0, 0)
	})

	// case4 stop-before-fire: stopping a pending timer removes both the
	// timer registry entry and reverse-map entry.
	t.Run("case4 stop-before-fire", func(t *testing.T) {
		d, _, ctx, cancel := newDispatcherWithCtx(t)
		defer stopDispatcher(t, d, cancel)

		result := startMatrixTimer(ctx, t, d, time.Hour, 2, func(at time.Time, gen uint64) payload.Payload {
			return payload.NewPayload(at.UnixNano(), payload.Golang)
		}, nil)
		stopped, err := d.stopTimerByID(result.ID)
		if err != nil {
			t.Fatalf("stopTimerByID: %v", err)
		}
		if !stopped {
			t.Fatal("expected pending timer to report stopped")
		}
		assertClockTimerMaps(t, d, 0, 0)
	})

	// case5 stop-after-fire(noop): stopping through the returned cleanup
	// function after natural fire is a no-op and leaves maps empty.
	t.Run("case5 stop-after-fire(noop)", func(t *testing.T) {
		d, node, ctx, cancel := newDispatcherWithCtx(t)
		defer stopDispatcher(t, d, cancel)

		result := startMatrixTimer(ctx, t, d, 5*time.Millisecond, 3, func(at time.Time, gen uint64) payload.Payload {
			return payload.NewPayload(at.UnixNano(), payload.Golang)
		}, nil)
		waitForClockPackages(t, node, 1)
		result.Stop()
		assertClockTimerMaps(t, d, 0, 0)
	})

	// case6 reset old-arm dropped: reset bumps the shared generation before
	// the new arm fires.
	t.Run("case6 reset old-arm dropped", func(t *testing.T) {
		d, node, ctx, cancel := newDispatcherWithCtx(t)
		defer stopDispatcher(t, d, cancel)

		var gen atomic.Uint64
		result := startMatrixTimer(ctx, t, d, time.Hour, 4, func(at time.Time, gen uint64) payload.Payload {
			return payload.NewPayload(gen, payload.Golang)
		}, &gen)
		resetRcv := newMockReceiver()
		if err := d.handleTimerReset(ctx, clockapi.TimerResetCmd{TimerID: result.ID, Duration: 5 * time.Millisecond}, 2, resetRcv); err != nil {
			t.Fatal(err)
		}
		resetRcv.await(t, time.Second)
		if got := gen.Load(); got != 1 {
			t.Fatalf("gen after reset=%d, want 1", got)
		}
		pkgs := waitForClockPackages(t, node, 1)
		if got := pkgs[len(pkgs)-1].payloads[0].Data().(uint64); got != 1 {
			t.Fatalf("fired gen=%d, want 1", got)
		}
		assertClockTimerMaps(t, d, 0, 0)
	})

	// case8 drain-with-pending cancels dispatcher: dispatcher Stop cancels
	// pending timers and clears reverse maps.
	t.Run("case8 drain-with-pending cancels dispatcher", func(t *testing.T) {
		d, _, ctx, cancel := newDispatcherWithCtx(t)
		startMatrixTimer(ctx, t, d, time.Hour, 5, func(at time.Time, gen uint64) payload.Payload {
			return payload.NewPayload(at.UnixNano(), payload.Golang)
		}, nil)
		stopDispatcher(t, d, cancel)
		assertClockTimerMaps(t, d, 0, 0)
	})

	// case12 stop-before-start: an early StopByChID tombstone prevents the
	// late start from scheduling a timer.
	t.Run("case12 stop-before-start", func(t *testing.T) {
		d, _, ctx, cancel := newDispatcherWithCtx(t)
		defer stopDispatcher(t, d, cancel)

		stopRcv := newMockReceiver()
		if err := d.handleTimerStopByChID(ctx, clockapi.TimerStopByChIDCmd{TargetPID: samplePID("timer"), Epoch: 7, ChID: 6}, 1, stopRcv); err != nil {
			t.Fatal(err)
		}
		stopRcv.await(t, time.Second)

		startRcv := newMockReceiver()
		if err := d.handleTimerStart(ctx, clockapi.TimerStartCmd{
			PID: samplePID("timer"), Topic: "timer-topic", Duration: time.Hour, Epoch: 7, ChID: 6,
			Build: func(at time.Time, gen uint64) payload.Payload {
				return payload.NewPayload(at.UnixNano(), payload.Golang)
			},
		}, 2, startRcv); err != nil {
			t.Fatal(err)
		}
		startRcv.await(t, time.Second)
		_, err := startRcv.snapshot()
		if !errors.Is(err, clockapi.ErrStoppedBeforeStart) {
			t.Fatalf("late start err=%v, want ErrStoppedBeforeStart", err)
		}
		assertClockTimerMaps(t, d, 0, 0)
	})

	// case14 ticker stopped: stopping a router-tagged ticker removes both
	// registry and reverse-map entries.
	t.Run("case14 ticker stopped", func(t *testing.T) {
		d, _, ctx, cancel := newDispatcherWithCtx(t)
		defer stopDispatcher(t, d, cancel)

		rcv := newMockReceiver()
		if err := d.handleTickerStart(ctx, clockapi.TickerStartCmd{
			PID: samplePID("ticker"), Topic: "ticker-topic", Duration: 5 * time.Millisecond, Epoch: 7, ChID: 14,
			Build: func(at time.Time, gen uint64) payload.Payload {
				return payload.NewPayload(at.UnixNano(), payload.Golang)
			},
		}, 1, rcv); err != nil {
			t.Fatal(err)
		}
		rcv.await(t, time.Second)
		data, err := rcv.snapshot()
		if err != nil {
			t.Fatal(err)
		}
		result := data.(clockapi.TickerStartResult)
		result.Stop()
		if got := d.TickerCount(); got != 0 {
			t.Fatalf("TickerCount=%d, want 0", got)
		}
		_, reverse := d.ReverseMapSize()
		if reverse != 0 {
			t.Fatalf("ticker reverse map=%d, want 0", reverse)
		}
	})

	// case31 dispatcher callback panic still cleans maps: a panic in the
	// fire builder is recovered and all timer bookkeeping is released.
	t.Run("case31 dispatcher callback panic still cleans maps", func(t *testing.T) {
		d, _, ctx, cancel := newDispatcherWithCtx(t)
		defer stopDispatcher(t, d, cancel)

		startMatrixTimer(ctx, t, d, 5*time.Millisecond, 31, func(at time.Time, gen uint64) payload.Payload {
			panic("boom")
		}, nil)
		time.Sleep(30 * time.Millisecond)
		assertClockTimerMaps(t, d, 0, 0)
	})

	// case33 zero/negative duration same terminal path: immediate one-shot
	// timers with builders complete their yield, emit a terminal frame, and
	// leave no dispatcher maps behind.
	t.Run("case33 zero/negative duration same terminal path", func(t *testing.T) {
		for _, duration := range []time.Duration{0, -time.Millisecond} {
			d, node, ctx, cancel := newDispatcherWithCtx(t)
			result := startMatrixTimer(ctx, t, d, duration, 33, func(at time.Time, gen uint64) payload.Payload {
				return payload.NewPayload(at.UnixNano(), payload.Golang)
			}, nil)
			if result.ID != 0 {
				t.Fatalf("immediate timer ID=%d, want 0", result.ID)
			}
			pkgs := waitForClockPackages(t, node, 1)
			last := pkgs[len(pkgs)-1]
			if len(last.payloads) < 2 || !payload.IsTerminal(last.payloads[len(last.payloads)-1]) {
				t.Fatalf("immediate timer payloads = %#v, want terminal", last.payloads)
			}
			assertClockTimerMaps(t, d, 0, 0)
			stopDispatcher(t, d, cancel)
		}
	})
}
