// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"runtime"
	"sync/atomic"
	"testing"

	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
)

// Stress coverage for the ephemeral channel router.
//
// These tests target the original production failure mode: long-running
// processes (scheduler / job_worker) calling time.after on every poll
// iteration accumulated GBs of subs/handlers/cleanup entries. The
// router replaces per-call topic subscriptions; the assertion under
// load is that no map grows with iteration count.

const stressIterations = 100_000

// TestEphemeralStress_RegisterFireClose runs many register / value /
// close cycles back-to-back and verifies that the router map returns
// to zero entries after each cycle, and that subs.byTopic never gains
// an entry from the router path.
func TestEphemeralStress_RegisterFireClose(t *testing.T) {
	if testing.Short() {
		t.Skip("stress test skipped under -short")
	}

	proc := startEphemeralProcess(t, `
		local hold = channel.new(1)
		subscribe("hold", hold)
		hold:receive()
		return "ok"
	`)
	defer proc.Close()
	ephemeralRunUntilIdle(t, proc)

	for i := 0; i < stressIterations; i++ {
		ch := NewChannel(1)
		chID, epoch, _ := proc.RegisterEphemeral(ch, nil, nil, OverflowDrop)
		sendEphemeralFrame(t, proc, &EphemeralFrame{
			Epoch:    epoch,
			ChID:     chID,
			Gen:      0,
			HasValue: true,
			Close:    true,
			Payloads: payload.Payloads{payload.NewPayload(lua.LString("tick"), payload.Lua)},
		})
		if !ch.IsClosed() {
			t.Fatalf("iter %d: channel must be closed after HasValue+Close frame", i)
		}
		if i%10_000 == 0 {
			// Periodic sanity check — router stays at exactly the count of
			// currently-active ephemerals (zero here because every cycle
			// closes immediately).
			if got := proc.router.Load().size(); got != 0 {
				t.Fatalf("router leaked entries at iter %d: size=%d", i, got)
			}
		}
	}

	if got := proc.router.Load().size(); got != 0 {
		t.Fatalf("router not empty after stress: size=%d", got)
	}

	// The router never installs a subs entry — verify subs map sizes
	// match what the script holds (1 user subscription: "hold").
	proc.subs.mu.RLock()
	byTopicLen := len(proc.subs.byTopic)
	byChannelLen := len(proc.subs.byChannel)
	proc.subs.mu.RUnlock()
	if byTopicLen != 1 || byChannelLen != 1 {
		t.Errorf("subs maps grew under stress: byTopic=%d byChannel=%d (want 1)", byTopicLen, byChannelLen)
	}
	if len(proc.handlers) != 0 {
		t.Errorf("handlers map should remain empty, got %d entries", len(proc.handlers))
	}

	// Release the script.
	var output process.StepOutput
	if err := proc.Step([]process.Event{{
		Type: process.EventMessage,
		Data: &relay.Package{Messages: []*relay.Message{{
			Topic: "hold", Payloads: payload.Payloads{payload.NewPayload(lua.LString("go"), payload.Lua)},
		}}},
	}}, &output); err != nil {
		t.Fatal(err)
	}
	ephemeralRunUntilDone(t, proc)
}

// TestEphemeralStress_HeapStable measures heap-in-use delta after a
// large register/close run with explicit GC. The fix MUST keep
// growth bounded — pre-fix this loop accumulated tens of MB per 100k
// iterations.
func TestEphemeralStress_HeapStable(t *testing.T) {
	if testing.Short() {
		t.Skip("stress test skipped under -short")
	}

	proc := startEphemeralProcess(t, `
		local hold = channel.new(1)
		subscribe("hold", hold)
		hold:receive()
		return "ok"
	`)
	defer proc.Close()
	ephemeralRunUntilIdle(t, proc)

	// Warm up: run a few thousand iterations so steady-state allocations
	// settle.
	const warmup = 5_000
	for i := 0; i < warmup; i++ {
		ch := NewChannel(1)
		chID, epoch, _ := proc.RegisterEphemeral(ch, nil, nil, OverflowDrop)
		sendEphemeralFrame(t, proc, &EphemeralFrame{
			Epoch: epoch, ChID: chID, Gen: 0, HasValue: true, Close: true,
			Payloads: payload.Payloads{payload.NewPayload(lua.LString("warm"), payload.Lua)},
		})
	}

	runtime.GC()
	var beforeMS runtime.MemStats
	runtime.ReadMemStats(&beforeMS)

	const measureIters = 50_000
	for i := 0; i < measureIters; i++ {
		ch := NewChannel(1)
		chID, epoch, _ := proc.RegisterEphemeral(ch, nil, nil, OverflowDrop)
		sendEphemeralFrame(t, proc, &EphemeralFrame{
			Epoch: epoch, ChID: chID, Gen: 0, HasValue: true, Close: true,
			Payloads: payload.Payloads{payload.NewPayload(lua.LString("tick"), payload.Lua)},
		})
	}

	runtime.GC()
	runtime.GC()
	var afterMS runtime.MemStats
	runtime.ReadMemStats(&afterMS)

	delta := int64(afterMS.HeapInuse) - int64(beforeMS.HeapInuse)
	t.Logf("HeapInuse delta over %d iterations: %d bytes (%.2f bytes/iter)",
		measureIters, delta, float64(delta)/float64(measureIters))

	// Generous bound: 2 MB of net growth across 50k iterations would
	// indicate per-iteration retention of ~40 bytes, which is enough to
	// reproduce the original GB-scale leak over hours. The fix should
	// hold much tighter than this.
	const maxAllowedDelta = 2 * 1024 * 1024
	if delta > maxAllowedDelta {
		t.Fatalf("heap growth %d bytes exceeds %d-byte bound after %d iterations", delta, maxAllowedDelta, measureIters)
	}

	if got := proc.router.Load().size(); got != 0 {
		t.Errorf("router not empty after stress: size=%d", got)
	}

	// Release the script.
	var output process.StepOutput
	if err := proc.Step([]process.Event{{
		Type: process.EventMessage,
		Data: &relay.Package{Messages: []*relay.Message{{
			Topic: "hold", Payloads: payload.Payloads{payload.NewPayload(lua.LString("go"), payload.Lua)},
		}}},
	}}, &output); err != nil {
		t.Fatal(err)
	}
	ephemeralRunUntilDone(t, proc)
}

// TestEphemeralStress_MultiCoroutineTimerSelect verifies that many
// Lua coroutines each waiting on their OWN ephemeral channel are
// woken correctly when their respective frames arrive, and that
// channel block/release refcounts (p.channels) settle back to zero
// after every fire-and-close cycle.
func TestEphemeralStress_MultiCoroutineTimerSelect(t *testing.T) {
	if testing.Short() {
		t.Skip("stress test skipped under -short")
	}

	const N = 32

	// Phase 1: script waits on the "start" signal so the Go side can
	// inject the channels table before any coroutine reads from it.
	proc := startEphemeralProcess(t, `
		local start = channel.new(1)
		subscribe("start", start)
		local control = channel.new(1)
		subscribe("control", control)

		start:receive()

		_G.received = 0

		for i = 1, 32 do
			coroutine.spawn(function()
				local ch = _G.channels[i]
				local v, ok = ch:receive()
				if ok then
					_G.received = _G.received + 1
				end
			end)
		end
		control:receive()
		return _G.received
	`)
	defer proc.Close()

	// Drive Lua until the main task is blocked on the "start" channel.
	ephemeralRunUntilIdle(t, proc)

	// Allocate channels, inject them into _G.channels.
	channels := make([]*Channel, N)
	chIDs := make([]uint64, N)
	chTable := proc.state.NewTable()
	for i := 0; i < N; i++ {
		channels[i] = NewChannel(1)
		ud := PushChannel(proc.state, channels[i])
		chTable.RawSetInt(i+1, ud)
		proc.state.Pop(1)
	}
	proc.state.SetGlobal("channels", chTable)

	// Unblock main so it spawns the receivers.
	var output process.StepOutput
	if err := proc.Step([]process.Event{{
		Type: process.EventMessage,
		Data: &relay.Package{Messages: []*relay.Message{{
			Topic: "start", Payloads: payload.Payloads{payload.NewPayload(lua.LString("go"), payload.Lua)},
		}}},
	}}, &output); err != nil {
		t.Fatal(err)
	}
	ephemeralRunUntilIdle(t, proc)

	// Each ephemeral channel is in recvq with a coroutine blocked on it,
	// plus the main task is blocked on the user "control" channel.
	// p.channels should now hold N+1 entries.
	if got := len(proc.channels); got != N+1 {
		t.Fatalf("expected %d channels in p.channels after spawn, got %d", N+1, got)
	}

	// Register all channels with the router (epoch is constant for this run).
	epoch := proc.epoch.Load()
	for i, ch := range channels {
		id, e, _ := proc.RegisterEphemeral(ch, nil, nil, OverflowDrop)
		if e != epoch {
			t.Fatalf("epoch changed mid-stress: was %d, got %d", epoch, e)
		}
		chIDs[i] = id
	}

	// Send a frame to each chID. Each frame is a value-and-close so
	// the router auto-closes the channel and removes the entry.
	for i := 0; i < N; i++ {
		sendEphemeralFrame(t, proc, &EphemeralFrame{
			Epoch:    epoch,
			ChID:     chIDs[i],
			Gen:      0,
			HasValue: true,
			Close:    true,
			Payloads: payload.Payloads{payload.NewPayload(lua.LString("tick"), payload.Lua)},
		})
	}

	// Step until every coroutine has run and the main task is blocked
	// again on control.
	ephemeralRunUntilIdle(t, proc)

	if got := proc.router.Load().size(); got != 0 {
		t.Errorf("router should be empty after %d fire-and-close frames, got %d", N, got)
	}
	// Only the control-channel block should remain (main task still
	// blocked on it). Every ephemeral channel's refcount must have
	// gone back to zero.
	if got := len(proc.channels); got != 1 {
		t.Errorf("p.channels should drain to 1 (just 'control') after every receiver wakes, got %d", got)
	}

	// Verify the global counter shows all coroutines received.
	var got int
	switch v := proc.state.GetGlobal("received").(type) {
	case lua.LNumber:
		got = int(v)
	case lua.LInteger:
		got = int(v)
	default:
		t.Fatalf("expected received to be numeric, got %T %v", v, v)
	}
	if got != N {
		t.Errorf("expected received=%d, got %d", N, got)
	}

	// Release the script.
	output.Reset()
	if err := proc.Step([]process.Event{{
		Type: process.EventMessage,
		Data: &relay.Package{Messages: []*relay.Message{{
			Topic: "control", Payloads: payload.Payloads{payload.NewPayload(lua.LString("go"), payload.Lua)},
		}}},
	}}, &output); err != nil {
		t.Fatal(err)
	}
	ephemeralRunUntilDone(t, proc)
}

// TestEphemeralStress_ProducerStopRaceWithAbort exercises the
// production lifecycle path: Lua HandleResult calls
// SetEphemeralProducerStop after RegisterEphemeral, on the step
// goroutine; meanwhile Abort from the scheduler goroutine snapshots
// stop closures. The race detector should not flag the producerStop
// field access.
func TestEphemeralStress_ProducerStopRaceWithAbort(t *testing.T) {
	if testing.Short() {
		t.Skip("stress test skipped under -short")
	}

	proto, err := lua.CompileString(`
		local hold = channel.new(1)
		subscribe("hold", hold)
		hold:receive()
		return "ok"
	`, "ps_race.lua")
	if err != nil {
		t.Fatal(err)
	}

	proc := mustNewProcess(t, WithProto(proto))
	defer proc.Close()
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	LoadModuleDef(proc.State(), ChannelModule)
	loadPubSubGlobals(proc.State())
	ephemeralRunUntilIdle(t, proc)

	const iters = 5_000
	done := make(chan struct{})
	go func() {
		for i := 0; i < iters; i++ {
			proc.Abort()
		}
		close(done)
	}()

	for i := 0; i < iters; i++ {
		ch := NewChannel(1)
		chID, _, _ := proc.RegisterEphemeral(ch, nil, nil, OverflowDrop)
		// Production pattern: HandleResult attaches the dispatcher Stop
		// func after the start command returns.
		proc.SetEphemeralProducerStop(chID, func() {})
		// Tear down to keep the router small.
		proc.StopEphemeral(chID)
	}
	<-done

	// Release the script.
	var output process.StepOutput
	if err := proc.Step([]process.Event{{
		Type: process.EventMessage,
		Data: &relay.Package{Messages: []*relay.Message{{
			Topic: "hold", Payloads: payload.Payloads{payload.NewPayload(lua.LString("go"), payload.Lua)},
		}}},
	}}, &output); err != nil {
		t.Fatal(err)
	}
	ephemeralRunUntilDone(t, proc)
}

// TestEphemeralStress_ConcurrentEpochBumps drives many concurrent
// Abort()s + register/close cycles to confirm the atomic epoch + lock
// discipline holds under race detection.
func TestEphemeralStress_ConcurrentEpochBumps(t *testing.T) {
	if testing.Short() {
		t.Skip("stress test skipped under -short")
	}

	proto, err := lua.CompileString(`
		local hold = channel.new(1)
		subscribe("hold", hold)
		hold:receive()
		return "ok"
	`, "stress.lua")
	if err != nil {
		t.Fatal(err)
	}

	proc := mustNewProcess(t, WithProto(proto))
	defer proc.Close()
	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}
	LoadModuleDef(proc.State(), ChannelModule)
	loadPubSubGlobals(proc.State())
	ephemeralRunUntilIdle(t, proc)

	const aborts = 1_000
	var aborted atomic.Int64
	done := make(chan struct{})
	go func() {
		for i := 0; i < aborts; i++ {
			proc.Abort()
			aborted.Add(1)
		}
		close(done)
	}()

	for i := 0; i < 10_000; i++ {
		ch := NewChannel(1)
		// Register + send + close, all on step thread.
		chID, epoch, _ := proc.RegisterEphemeral(ch, nil, nil, OverflowDrop)
		sendEphemeralFrame(t, proc, &EphemeralFrame{
			Epoch: epoch, ChID: chID, Gen: 0, HasValue: true, Close: true,
			Payloads: payload.Payloads{payload.NewPayload(lua.LString("x"), payload.Lua)},
		})
	}

	<-done

	if got := aborted.Load(); got != aborts {
		t.Errorf("expected %d Abort() calls, got %d", aborts, got)
	}

	// Release the script.
	var output process.StepOutput
	if err := proc.Step([]process.Event{{
		Type: process.EventMessage,
		Data: &relay.Package{Messages: []*relay.Message{{
			Topic: "hold", Payloads: payload.Payloads{payload.NewPayload(lua.LString("go"), payload.Lua)},
		}}},
	}}, &output); err != nil {
		t.Fatal(err)
	}
	ephemeralRunUntilDone(t, proc)
}
