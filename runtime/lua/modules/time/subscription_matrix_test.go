// SPDX-License-Identifier: MPL-2.0

package time_test

import (
	"context"
	"testing"
	stdtime "time"

	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/runtime/lua/engine"
)

// End-to-end proof-matrix cases driven through the real time.* API on the
// scheduler + clock dispatcher. Each case asserts the dispatcher-side
// counts return to zero; the engine-internal
// TestSubscriptionLifecycleProofMatrix asserts the matching process subs /
// handlers map sizes under lock. Together they prove the subscription-owned
// cleanup path holds for both halves.

func newMatrixProcess(t *testing.T, script string) *engine.Process {
	t.Helper()
	proto, err := lua.CompileString(script, "subscription_matrix.lua")
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	proc, err := engine.NewProcess(
		engine.WithProto(proto),
		engine.WithModuleBinder(func(l *lua.LState) error {
			engine.LoadModuleDef(l, engine.ChannelModule)
			return nil
		}),
		engine.WithModuleBinder(bindTimeModule),
		engine.WithModuleBinder(func(l *lua.LState) error {
			l.SetGlobal("stdNs", lua.LNumber(stdtime.Millisecond))
			l.SetGlobal("longNs", lua.LNumber(int64(stdtime.Hour)))
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("NewProcess failed: %v", err)
	}
	return proc
}

func TestTimeSubscriptionLifecycleProofMatrix(t *testing.T) {
	// case3 discard-before-fire (bounded, ->0 after fire): a time.after
	// channel is created but never received from. The one-shot fire
	// delivers a terminal frame that reclaims the subscription without a
	// reader, so the dispatcher timer registry returns to zero. Repeated
	// in a loop to prove the buffers stay bounded.
	t.Run("case3 discard-before-fire", func(t *testing.T) {
		sched := newTestScheduler()
		sched.Start()
		defer sched.Stop()

		script := `
			for i = 1, 200 do
				-- Create a one-shot channel and discard it without reading.
				time.after(stdNs)
			end
			-- Park briefly so every discarded one-shot has a chance to fire
			-- and self-retire before the process exits.
			local guard = time.after(50 * stdNs)
			guard:receive()
			return "ok"
		`
		ctx, _ := ctxapi.OpenFrameContext(context.Background())
		proc := newMatrixProcess(t, script)
		if _, err := sched.Execute(ctx, testPID(), proc, "", nil); err != nil {
			t.Fatal(err)
		}
		if got := sched.clock.TimerCount(); got != 0 {
			t.Errorf("dispatcher leaked %d timers after discard-before-fire", got)
		}
		timers, _ := sched.clock.ReverseMapSize()
		if timers != 0 {
			t.Errorf("dispatcher timer reverse map = %d, want 0", timers)
		}
	})

	// case13 ticker-never-stopped (->0 on process drain): a ticker is
	// created and read, then the process exits WITHOUT calling :stop().
	// The process drain closes the subscription and runs its cleanup
	// closure (the dispatcher Stop), so the ticker registry returns to
	// zero.
	t.Run("case13 ticker-never-stopped", func(t *testing.T) {
		sched := newTestScheduler()
		sched.Start()
		defer sched.Stop()

		script := `
			local tk = time.ticker(stdNs)
			tk:response():receive()
			tk:response():receive()
			-- Exit without :stop(); drain must release the ticker.
			return "ok"
		`
		ctx, fc := ctxapi.OpenFrameContext(context.Background())
		proc := newMatrixProcess(t, script)
		if _, err := sched.Execute(ctx, testPID(), proc, "", nil); err != nil {
			t.Fatal(err)
		}
		ctxapi.ReleaseFrameContext(fc)
		waitTickerCount(t, sched, 0)
	})

	// case16 ticker-dropped-without-stop (bounded, ->0 on drain): a ticker
	// fires faster than the process reads. The bounded buffer drops missed
	// ticks instead of growing the message queue, and the process drain
	// still releases the ticker on exit.
	t.Run("case16 ticker-dropped-without-stop", func(t *testing.T) {
		sched := newTestScheduler()
		sched.Start()
		defer sched.Stop()

		script := `
			-- Fast ticker, slow reader: ticks accumulate against the bounded
			-- buffer and overflow is dropped, never queued unboundedly.
			local tk = time.ticker(stdNs)
			local slow = time.after(40 * stdNs)
			slow:receive()
			-- Read a single tick to prove the channel is still live, then
			-- exit without :stop().
			tk:response():receive()
			return "ok"
		`
		ctx, fc := ctxapi.OpenFrameContext(context.Background())
		proc := newMatrixProcess(t, script)
		if _, err := sched.Execute(ctx, testPID(), proc, "", nil); err != nil {
			t.Fatal(err)
		}
		ctxapi.ReleaseFrameContext(fc)
		waitTickerCount(t, sched, 0)
	})

	// case30 time.after in a select that loses: time.after competes with a
	// ready channel in channel.select. The select picks the ready branch,
	// the timer fires later, and its terminal frame reclaims the orphaned
	// one-shot subscription. Dispatcher timers return to zero.
	t.Run("case30 time.after in a losing select", func(t *testing.T) {
		sched := newTestScheduler()
		sched.Start()
		defer sched.Stop()

		script := `
			local ready = channel.new(1)
			ready:send("now")
			-- The deadline loses the select to the already-ready channel.
			local result = channel.select({
				ready:case_receive(),
				time.after(stdNs):case_receive(),
			})
			if result.value ~= "now" or result.ok ~= true then
				return nil, "expected ready channel to win"
			end
			-- Park long enough for the losing deadline to fire and retire.
			time.after(50 * stdNs):receive()
			return "ok"
		`
		ctx, _ := ctxapi.OpenFrameContext(context.Background())
		proc := newMatrixProcess(t, script)
		result, err := sched.Execute(ctx, testPID(), proc, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		if result == nil {
			t.Fatal("nil result")
		}
		if got := sched.clock.TimerCount(); got != 0 {
			t.Errorf("dispatcher leaked %d timers after losing select", got)
		}
	})
}

func waitTickerCount(t *testing.T, sched *testScheduler, want int) {
	t.Helper()
	deadline := stdtime.Now().Add(stdtime.Second)
	for {
		if got := sched.clock.TickerCount(); got == want {
			return
		}
		if stdtime.Now().After(deadline) {
			t.Fatalf("ticker count = %d, want %d", sched.clock.TickerCount(), want)
		}
		stdtime.Sleep(stdtime.Millisecond)
	}
}
