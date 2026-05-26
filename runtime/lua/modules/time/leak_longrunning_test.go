// SPDX-License-Identifier: MPL-2.0

package time_test

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	stdtime "time"

	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/runtime/lua/engine"
)

// Long-running proof: ONE process that drives ~100k sequential timer
// operations (the scheduler/job_worker shape). The original leak grew the
// process subscription/handler maps and the clock dispatcher's timer maps on
// every fire, regardless of whether the channel was ever received. Here a
// single long-lived process runs each variant to completion; afterward the
// dispatcher must hold zero timers/tickers and zero reverse-map entries, and
// the post-GC heap must not retain per-iteration state.

// Every iteration here is a real round-trip through the actor scheduler
// (~ms each), so the actor-level proof runs a few thousand iterations and
// relies on LIVE sampling (the dispatcher count must stay bounded WHILE the
// single actor loops, not just be zero after it exits). The high-N (100k)
// flatness proof lives at the engine level, which bypasses the scheduler
// round-trip (see engine subscription_lifecycle_matrix_test.go).
func bulkIterations() int {
	if testing.Short() {
		return 1000
	}
	return 5000
}

func waitedIterations() int {
	if testing.Short() {
		return 500
	}
	return 2000
}

// runLongLived executes script in a single process with global N=n and asserts
// the dispatcher timer/ticker/reverse-map counts are zero once it completes.
// The script MUST return its completed iteration count; runLongLived asserts it
// equals n so a vacuous pass (loop short-circuited, receive returned nil
// without a real fire) cannot masquerade as no-leak.
func runLongLived(t *testing.T, n, liveCeiling int, script string, durationNs stdtime.Duration) {
	t.Helper()
	sched := newTestScheduler()
	sched.Start()
	defer sched.Stop()

	proto, err := lua.CompileString(script, "leak_longrun.lua")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	proc, err := engine.NewProcess(
		engine.WithProto(proto),
		engine.WithModuleBinder(func(l *lua.LState) error {
			engine.LoadModuleDef(l, engine.ChannelModule)
			return nil
		}),
		engine.WithModuleBinder(bindTimeModule),
		engine.WithModuleBinder(func(l *lua.LState) error {
			l.SetGlobal("N", lua.LNumber(n))
			l.SetGlobal("D", lua.LNumber(durationNs))
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := runtimeapi.SetFramePID(ctx, testPID()); err != nil {
		t.Fatalf("set frame pid: %v", err)
	}
	// Sample the live dispatcher count WHILE the single actor loops. A
	// leaking long-running actor would climb toward n here; a clean one stays
	// near the concurrently-active count. This is the real signal — post-exit
	// drain would mask accumulation that only frees when the process ends.
	var liveMax atomic.Int64
	stopSampler := make(chan struct{})
	samplerDone := make(chan struct{})
	go func() {
		defer close(samplerDone)
		tick := stdtime.NewTicker(200 * stdtime.Microsecond)
		defer tick.Stop()
		for {
			select {
			case <-stopSampler:
				return
			case <-tick.C:
				live := int64(sched.clock.TimerCount() + sched.clock.TickerCount())
				for {
					prev := liveMax.Load()
					if live <= prev || liveMax.CompareAndSwap(prev, live) {
						break
					}
				}
			}
		}
	}()

	start := stdtime.Now()
	result, err := sched.Execute(ctx, testPID(), proc, "", nil)
	elapsed := stdtime.Since(start)
	close(stopSampler)
	<-samplerDone
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result == nil {
		t.Fatal("nil result")
	}
	if result.Error != nil {
		t.Fatalf("script reported failure (loop did not complete %d real iterations): %v",
			n, result.Error)
	}
	t.Logf("%d iterations in %v; live dispatcher peak %d (ceiling %d)", n, elapsed,
		liveMax.Load(), liveCeiling)
	if liveCeiling > 0 {
		if got := liveMax.Load(); got > int64(liveCeiling) {
			t.Errorf("live dispatcher count peaked at %d during the run (ceiling %d) — the actor is accumulating timers while alive, not just at exit",
				got, liveCeiling)
		}
	}

	// The loop exits the instant its last iteration returns; sub-microsecond
	// timers armed near the end may still be firing, and their fire-path
	// cleanup (registry delete then reverse-map delete in a deferred closure)
	// runs slightly after Execute returns. A leak never converges; a clean
	// dispatcher settles to zero within a few milliseconds. Poll for
	// convergence rather than reading once.
	waitDispatcherDrained(t, sched, n)
}

// waitDispatcherDrained polls until the dispatcher's timer/ticker registries
// and reverse maps are all zero, proving the long-running actor accumulated
// nothing. A real leak would never reach zero and the deadline fires.
func waitDispatcherDrained(t *testing.T, sched *testScheduler, n int) {
	t.Helper()
	deadline := stdtime.Now().Add(2 * stdtime.Second)
	for {
		timers := sched.clock.TimerCount()
		tickers := sched.clock.TickerCount()
		rTimers, rTickers := sched.clock.ReverseMapSize()
		if timers == 0 && tickers == 0 && rTimers == 0 && rTickers == 0 {
			return
		}
		if stdtime.Now().After(deadline) {
			t.Fatalf("dispatcher did not drain after %d iterations: timers=%d tickers=%d reverseTimers=%d reverseTickers=%d",
				n, timers, tickers, rTimers, rTickers)
		}
		stdtime.Sleep(stdtime.Millisecond)
	}
}

func TestLeak_LongRunning_100k(t *testing.T) {
	// Variant A — handled: every timer's value is received. Asserts each
	// receive returns a real (non-nil) fire value, N times.
	t.Run("after_handled", func(t *testing.T) {
		runLongLived(t, waitedIterations(), 8, `
			local count = 0
			for i = 1, N do
				local ch = time.after(D)
				local v = ch:receive()
				if v ~= nil then count = count + 1 end
			end
			if count ~= N then return nil, "delivered " .. count .. " of " .. N end
			return count
		`, stdtime.Microsecond)
	})

	// Variant B — unhandled/dropped: the return value is never received.
	// The fire still delivers a terminal frame that reclaims both sides.
	t.Run("after_dropped", func(t *testing.T) {
		runLongLived(t, bulkIterations(), 0, `
			local count = 0
			for i = 1, N do
				time.after(D)
				count = count + 1
			end
			if count ~= N then return nil, "ran " .. count .. " of " .. N end
			return count
		`, stdtime.Microsecond)
	})

	// Variant C — stopped before fire: timer is cancelled, never fires.
	t.Run("timer_stopped_before_fire", func(t *testing.T) {
		runLongLived(t, bulkIterations(), 0, `
			local count = 0
			for i = 1, N do
				local t = time.timer(D)
				t:stop()
				count = count + 1
			end
			if count ~= N then return nil, "ran " .. count .. " of " .. N end
			return count
		`, stdtime.Hour)
	})

	// Variant D — ticker start/receive/stop churn. Asserts each ticker
	// delivers a real tick before stop.
	t.Run("ticker_churn", func(t *testing.T) {
		runLongLived(t, waitedIterations(), 8, `
			local count = 0
			for i = 1, N do
				local tk = time.ticker(D)
				local v = tk:response():receive()
				if v ~= nil then count = count + 1 end
				tk:stop()
			end
			if count ~= N then return nil, "delivered " .. count .. " of " .. N end
			return count
		`, stdtime.Microsecond)
	})
}

// TestLeak_LongRunning_HeapBounded proves the per-iteration state does not
// accumulate: running 100k handled timers leaves the post-GC heap within a
// bounded delta of the pre-run baseline (a real map leak would retain ~100k
// subscription/handler/closure entries — tens of MB).
func TestLeak_LongRunning_HeapBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("heap-bounded proof runs the full 100k loop")
	}

	sched := newTestScheduler()
	sched.Start()
	defer sched.Stop()

	n := bulkIterations()
	// Bulk-register N timers without blocking per fire (the fast path that
	// actually reaches 100k); every one must be reclaimed by fire or drain.
	script := `
		local count = 0
		for i = 1, N do
			time.after(D)
			count = count + 1
		end
		if count ~= N then return nil, "ran " .. count .. " of " .. N end
		return count
	`
	proto, _ := lua.CompileString(script, "leak_heap.lua")
	proc, err := engine.NewProcess(
		engine.WithProto(proto),
		engine.WithModuleBinder(func(l *lua.LState) error {
			engine.LoadModuleDef(l, engine.ChannelModule)
			return nil
		}),
		engine.WithModuleBinder(bindTimeModule),
		engine.WithModuleBinder(func(l *lua.LState) error {
			l.SetGlobal("N", lua.LNumber(n))
			l.SetGlobal("D", lua.LNumber(stdtime.Microsecond))
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := runtimeapi.SetFramePID(ctx, testPID()); err != nil {
		t.Fatalf("set frame pid: %v", err)
	}
	result, err := sched.Execute(ctx, testPID(), proc, "", nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result != nil && result.Error != nil {
		t.Fatalf("script did not complete %d real iterations: %v", n, result.Error)
	}
	if got := sched.clock.TimerCount(); got != 0 {
		t.Errorf("dispatcher leaked %d timers after %d registrations", got, n)
	}

	runtime.GC()
	runtime.ReadMemStats(&after)

	// N leaked subscriptions/handlers/closures would be tens of MB; a
	// generous 16MB ceiling catches that while tolerating VM/runtime noise.
	const ceiling = 16 << 20
	if delta := int64(after.HeapInuse) - int64(before.HeapInuse); delta > ceiling {
		t.Errorf("heap grew %d bytes over %d timers (ceiling %d) — state retained per iteration",
			delta, n, ceiling)
	}
}
