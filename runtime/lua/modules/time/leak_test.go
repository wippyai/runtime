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

// Regression tests for the scheduler/job_worker memory leak: per-call
// topic subscriptions in time.after / time.timer / time.ticker grew the
// process subs / handlers maps without bound for long-running processes.
//
// After the ephemeral channel router migration the maps must stay flat
// regardless of how many timers fire across the process's lifetime.

// TestLeak_TimeAfter_LoopCompletes runs the scheduler/job_worker's
// time.after(poll_interval) loop pattern at scale. The point is to
// drive the router code path through real channel.select + relay
// delivery, not to introspect maps (the engine-level
// TestEphemeral_ScalesUnderLongRunning already asserts that map sizes
// stay flat). If the migration broke message routing, this test would
// hang or fail to complete.
func TestLeak_TimeAfter_LoopCompletes(t *testing.T) {
	sched := newTestScheduler()
	sched.Start()
	defer sched.Stop()

	script := `
		local count = 0
		for i = 1, 500 do
			local ch = time.after(stdNs)
			ch:receive()
			count = count + 1
		end
		return count
	`

	proto, _ := lua.CompileString(script, "leak_after.lua")
	proc, err := engine.NewProcess(
		engine.WithProto(proto),
		engine.WithModuleBinder(func(l *lua.LState) error {
			engine.LoadModuleDef(l, engine.ChannelModule)
			return nil
		}),
		engine.WithModuleBinder(bindTimeModule),
		engine.WithModuleBinder(func(l *lua.LState) error {
			l.SetGlobal("stdNs", lua.LNumber(stdtime.Millisecond))
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if _, err := sched.Execute(ctx, testPID(), proc, "", nil); err != nil {
		t.Fatal(err)
	}

	// Dispatcher must be clean once the script returns. Any leftover
	// timer means a router entry was orphaned mid-flight.
	if got := sched.clock.TimerCount(); got != 0 {
		t.Errorf("dispatcher leaked %d active timers after 500 time.after cycles", got)
	}
}

// TestLeak_TimeTicker_StopUnsubscribes verifies time.ticker(...):stop()
// fully releases the router entry, the dispatcher reverse-map entry,
// and the Go ticker goroutine.
func TestLeak_TimeTicker_StopUnsubscribes(t *testing.T) {
	sched := newTestScheduler()
	sched.Start()
	defer sched.Stop()

	script := `
		for i = 1, 100 do
			local tk = time.ticker(stdNs)
			local received = false
			for j = 1, 2 do
				tk:response():receive()
				received = true
			end
			tk:stop()
			if not received then
				return nil, "ticker " .. i .. " did not deliver"
			end
		end
		return "ok"
	`
	proto, _ := lua.CompileString(script, "leak_ticker.lua")
	proc, err := engine.NewProcess(
		engine.WithProto(proto),
		engine.WithModuleBinder(func(l *lua.LState) error {
			engine.LoadModuleDef(l, engine.ChannelModule)
			return nil
		}),
		engine.WithModuleBinder(bindTimeModule),
		engine.WithModuleBinder(func(l *lua.LState) error {
			l.SetGlobal("stdNs", lua.LNumber(stdtime.Millisecond))
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if _, err := sched.Execute(ctx, testPID(), proc, "", nil); err != nil {
		t.Fatal(err)
	}

	// The dispatcher's ticker registry should be empty after every
	// :stop() returns. If it isn't, ticker goroutines leaked.
	if got := sched.clock.TickerCount(); got != 0 {
		t.Errorf("dispatcher leaked %d active tickers after 100 start/stop cycles", got)
	}
}

// TestLeak_TimeTimer_StopAndFireBothClean verifies both lifecycle
// terminations on time.timer don't leak: explicit stop AND natural fire.
func TestLeak_TimeTimer_StopAndFireBothClean(t *testing.T) {
	sched := newTestScheduler()
	sched.Start()
	defer sched.Stop()

	script := `
		-- Half: let timer fire and consume the value.
		for i = 1, 50 do
			local tm = time.timer(stdNs)
			tm:response():receive()
		end
		-- Half: stop before fire.
		for i = 1, 50 do
			local tm = time.timer(longNs)
			tm:stop()
		end
		return "ok"
	`
	proto, _ := lua.CompileString(script, "leak_timer.lua")
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
		t.Fatal(err)
	}

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if _, err := sched.Execute(ctx, testPID(), proc, "", nil); err != nil {
		t.Fatal(err)
	}

	// After 100 timers (50 fired + 50 stopped) the dispatcher must be
	// empty. The drain on Close also bumps the epoch so anything still
	// in flight is dropped on the (now-dead) process.
	if got := sched.clock.TimerCount(); got != 0 {
		t.Errorf("dispatcher leaked %d active timers after 100 start/fire-or-stop cycles", got)
	}
	timers, tickers := sched.clock.ReverseMapSize()
	if timers != 0 || tickers != 0 {
		t.Errorf("dispatcher reverse maps not empty: timers=%d tickers=%d", timers, tickers)
	}
}
