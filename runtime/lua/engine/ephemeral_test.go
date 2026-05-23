// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"sync/atomic"
	"testing"

	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
)

// Ephemeral channel router infrastructure tests.
//
// The router is fed via the same relay/Step path producers will use:
//   relay.Package{Topic: TopicEphemeral, Payloads: [Payload{EphemeralFrame}]}
// arrives, is queued by Step, and dispatched by deliverMessage's reserved-
// topic branch.

func startEphemeralProcess(t *testing.T, script string) *Process {
	t.Helper()

	proto, err := lua.CompileString(script, "ephemeral.lua")
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	proc := mustNewProcess(t, WithProto(proto))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}

	LoadModuleDef(proc.State(), ChannelModule)
	loadPubSubGlobals(proc.State())

	return proc
}

func ephemeralRunUntilIdle(t *testing.T, proc *Process) {
	t.Helper()
	var output process.StepOutput
	const maxSteps = 50
	for i := 0; i < maxSteps; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("step %d failed: %v", i, err)
		}
		if output.Status() == process.StepIdle || output.Status() == process.StepDone {
			return
		}
	}
	t.Fatalf("did not reach idle in %d steps", maxSteps)
}

func ephemeralRunUntilDone(t *testing.T, proc *Process) {
	t.Helper()
	var output process.StepOutput
	const maxSteps = 200
	for i := 0; i < maxSteps; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("step %d failed: %v", i, err)
		}
		if output.Status() == process.StepDone {
			return
		}
	}
	t.Fatalf("did not reach done in %d steps", maxSteps)
}

func sendEphemeralFrame(t *testing.T, proc *Process, frame *EphemeralFrame) {
	t.Helper()
	var output process.StepOutput
	events := []process.Event{{
		Type: process.EventMessage,
		Data: &relay.Package{
			Messages: []*relay.Message{{
				Topic:    TopicEphemeral,
				Payloads: payload.Payloads{NewEphemeralFramePayload(frame)},
			}},
		},
	}}
	output.Reset()
	if err := proc.Step(events, &output); err != nil {
		t.Fatalf("send ephemeral frame failed: %v", err)
	}
}

// TestEphemeral_NoSubscriptionEntry confirms the router never installs an
// entry in subs.byTopic or proc.handlers. Lifetime maps stay at zero
// regardless of how many ephemerals are registered.
func TestEphemeral_NoSubscriptionEntry(t *testing.T) {
	script := `
		-- Block forever so the test can inspect proc state before
		-- clearExecution wipes the maps.
		local hold = channel.new(1)
		subscribe("hold", hold)
		hold:receive()
		return "ok"
	`
	proc := startEphemeralProcess(t, script)
	defer proc.Close()

	ephemeralRunUntilIdle(t, proc)

	// Register a bunch of ephemerals.
	for i := 0; i < 100; i++ {
		ch := NewChannel(1)
		_, _ = proc.RegisterEphemeral(ch, nil, nil, OverflowDrop)
	}

	// Subscription maps must contain ONLY the user-level "hold" topic.
	proc.subs.mu.RLock()
	byTopicLen := len(proc.subs.byTopic)
	byChannelLen := len(proc.subs.byChannel)
	_, holdExists := proc.subs.byTopic["hold"]
	_, routeExists := proc.subs.byTopic[TopicEphemeral]
	proc.subs.mu.RUnlock()

	if byTopicLen != 1 || byChannelLen != 1 {
		t.Errorf("expected subs maps len 1 (only 'hold'); got byTopic=%d byChannel=%d", byTopicLen, byChannelLen)
	}
	if !holdExists {
		t.Error("expected 'hold' user subscription to still be present")
	}
	if routeExists {
		t.Error("TopicEphemeral must NEVER appear in subs.byTopic")
	}

	if proc.router == nil || proc.router.size() != 100 {
		size := 0
		if proc.router != nil {
			size = proc.router.size()
		}
		t.Errorf("router should have 100 entries, got %d", size)
	}

	// Let the script exit cleanly.
	var output process.StepOutput
	events := []process.Event{{
		Type: process.EventMessage,
		Data: &relay.Package{Messages: []*relay.Message{{Topic: "hold", Payloads: payload.Payloads{payload.NewPayload(lua.LString("go"), payload.Lua)}}}},
	}}
	output.Reset()
	if err := proc.Step(events, &output); err != nil {
		t.Fatal(err)
	}
	ephemeralRunUntilDone(t, proc)
}

// TestEphemeral_DeliverValueFrame: a frame with HasValue=true lands on the
// registered channel.
func TestEphemeral_DeliverValueFrame(t *testing.T) {
	script := `
		local hold = channel.new(1)
		subscribe("hold", hold)
		hold:receive()
		return "ok"
	`
	proc := startEphemeralProcess(t, script)
	defer proc.Close()
	ephemeralRunUntilIdle(t, proc)

	ch := NewChannel(2)
	chID, epoch := proc.RegisterEphemeral(ch, nil, nil, OverflowDrop)

	sendEphemeralFrame(t, proc, &EphemeralFrame{
		Epoch:    epoch,
		ChID:     chID,
		Gen:      0,
		HasValue: true,
		Payloads: payload.Payloads{payload.NewPayload(lua.LString("hi"), payload.Lua)},
	})

	if ch.buffer.Len() != 1 {
		t.Fatalf("value frame did not land in channel buffer, len=%d", ch.buffer.Len())
	}
	got := ch.buffer.Front().Value
	if s, ok := got.(lua.LString); !ok || string(s) != "hi" {
		t.Fatalf("buffer head wrong: %v", got)
	}

	// Drop the entry by closing.
	sendEphemeralFrame(t, proc, &EphemeralFrame{
		Epoch: epoch, ChID: chID, Gen: 0, Close: true,
	})
	if proc.router.size() != 0 {
		t.Fatalf("entry not removed after close frame: size=%d", proc.router.size())
	}
	if !ch.IsClosed() {
		t.Error("channel should be closed after close frame")
	}

	// Release the script.
	var output process.StepOutput
	events := []process.Event{{
		Type: process.EventMessage,
		Data: &relay.Package{Messages: []*relay.Message{{Topic: "hold", Payloads: payload.Payloads{payload.NewPayload(lua.LString("go"), payload.Lua)}}}},
	}}
	output.Reset()
	if err := proc.Step(events, &output); err != nil {
		t.Fatal(err)
	}
	ephemeralRunUntilDone(t, proc)
}

// TestEphemeral_StaleEpochDropped: a frame from a previous incarnation is
// silently dropped without touching the channel.
func TestEphemeral_StaleEpochDropped(t *testing.T) {
	script := `
		local hold = channel.new(1)
		subscribe("hold", hold)
		hold:receive()
		return "ok"
	`
	proc := startEphemeralProcess(t, script)
	defer proc.Close()
	ephemeralRunUntilIdle(t, proc)

	ch := NewChannel(2)
	chID, currentEpoch := proc.RegisterEphemeral(ch, nil, nil, OverflowDrop)

	sendEphemeralFrame(t, proc, &EphemeralFrame{
		Epoch:    currentEpoch + 7, // stale (or future)
		ChID:     chID,
		Gen:      0,
		HasValue: true,
		Payloads: payload.Payloads{payload.NewPayload(lua.LString("stale"), payload.Lua)},
	})

	if ch.buffer.Len() != 0 {
		t.Fatalf("stale-epoch frame must be dropped, channel got: %v", ch.buffer.Front().Value)
	}

	// Release the script.
	var output process.StepOutput
	events := []process.Event{{
		Type: process.EventMessage,
		Data: &relay.Package{Messages: []*relay.Message{{Topic: "hold", Payloads: payload.Payloads{payload.NewPayload(lua.LString("go"), payload.Lua)}}}},
	}}
	output.Reset()
	if err := proc.Step(events, &output); err != nil {
		t.Fatal(err)
	}
	ephemeralRunUntilDone(t, proc)
}

// TestEphemeral_StaleGenDropped: BumpEphemeralGen advances the entry; a
// frame with the old gen is dropped.
func TestEphemeral_StaleGenDropped(t *testing.T) {
	script := `
		local hold = channel.new(1)
		subscribe("hold", hold)
		hold:receive()
		return "ok"
	`
	proc := startEphemeralProcess(t, script)
	defer proc.Close()
	ephemeralRunUntilIdle(t, proc)

	ch := NewChannel(2)
	chID, epoch := proc.RegisterEphemeral(ch, nil, nil, OverflowDrop)

	newGen, ok := proc.BumpEphemeralGen(chID)
	if !ok || newGen != 1 {
		t.Fatalf("expected BumpEphemeralGen → 1, got (%d, %v)", newGen, ok)
	}

	// Frame from prior arm (gen=0).
	sendEphemeralFrame(t, proc, &EphemeralFrame{
		Epoch:    epoch,
		ChID:     chID,
		Gen:      0,
		HasValue: true,
		Payloads: payload.Payloads{payload.NewPayload(lua.LString("stale"), payload.Lua)},
	})
	if ch.buffer.Len() != 0 {
		t.Fatalf("stale-gen frame must be dropped, channel head: %v", ch.buffer.Front().Value)
	}

	// Frame from new arm (gen=1) lands.
	sendEphemeralFrame(t, proc, &EphemeralFrame{
		Epoch:    epoch,
		ChID:     chID,
		Gen:      1,
		HasValue: true,
		Payloads: payload.Payloads{payload.NewPayload(lua.LString("fresh"), payload.Lua)},
	})
	if ch.buffer.Len() != 1 {
		t.Fatalf("new-gen frame must land, buffer len %d", ch.buffer.Len())
	}

	// Release the script.
	var output process.StepOutput
	events := []process.Event{{
		Type: process.EventMessage,
		Data: &relay.Package{Messages: []*relay.Message{{Topic: "hold", Payloads: payload.Payloads{payload.NewPayload(lua.LString("go"), payload.Lua)}}}},
	}}
	output.Reset()
	if err := proc.Step(events, &output); err != nil {
		t.Fatal(err)
	}
	ephemeralRunUntilDone(t, proc)
}

// TestEphemeral_UnknownChIDDropped: a frame for a chID that was never
// registered (or already drained) is silently dropped.
func TestEphemeral_UnknownChIDDropped(t *testing.T) {
	script := `
		local hold = channel.new(1)
		subscribe("hold", hold)
		hold:receive()
		return "ok"
	`
	proc := startEphemeralProcess(t, script)
	defer proc.Close()
	ephemeralRunUntilIdle(t, proc)

	// Initialize the router with one real entry so router != nil.
	ch := NewChannel(2)
	_, epoch := proc.RegisterEphemeral(ch, nil, nil, OverflowDrop)

	sendEphemeralFrame(t, proc, &EphemeralFrame{
		Epoch:    epoch,
		ChID:     999999, // never registered
		Gen:      0,
		HasValue: true,
		Payloads: payload.Payloads{payload.NewPayload(lua.LString("ghost"), payload.Lua)},
	})

	if ch.buffer.Len() != 0 {
		t.Fatal("unknown-chID frame must not touch any channel")
	}
	if proc.router.size() != 1 {
		t.Fatalf("router size should still be 1, got %d", proc.router.size())
	}

	// Release the script.
	var output process.StepOutput
	events := []process.Event{{
		Type: process.EventMessage,
		Data: &relay.Package{Messages: []*relay.Message{{Topic: "hold", Payloads: payload.Payloads{payload.NewPayload(lua.LString("go"), payload.Lua)}}}},
	}}
	output.Reset()
	if err := proc.Step(events, &output); err != nil {
		t.Fatal(err)
	}
	ephemeralRunUntilDone(t, proc)
}

// TestEphemeral_DrainCallsProducerStopOnce ensures producerStop runs
// exactly once across multiple drain triggers (Drain + Close + Init).
func TestEphemeral_DrainCallsProducerStopOnce(t *testing.T) {
	script := `
		local hold = channel.new(1)
		subscribe("hold", hold)
		hold:receive()
		return "ok"
	`
	proc := startEphemeralProcess(t, script)
	defer proc.Close()
	ephemeralRunUntilIdle(t, proc)

	var stopCount int32
	stop := func() { atomic.AddInt32(&stopCount, 1) }
	ch := NewChannel(1)
	proc.RegisterEphemeral(ch, nil, stop, OverflowDrop)

	// Drain once via the internal helper.
	proc.drainEphemeralChannels()
	if got := atomic.LoadInt32(&stopCount); got != 1 {
		t.Fatalf("after first drain expected stop=1, got %d", got)
	}

	// Re-register then drain via Close.
	stopCount = 0
	ch2 := NewChannel(1)
	proc.RegisterEphemeral(ch2, nil, stop, OverflowDrop)

	proc.drainEphemeralChannels()
	proc.drainEphemeralChannels() // second drain must be a no-op
	if got := atomic.LoadInt32(&stopCount); got != 1 {
		t.Fatalf("second drain should not re-stop, got count=%d", got)
	}

	// Release the script.
	var output process.StepOutput
	events := []process.Event{{
		Type: process.EventMessage,
		Data: &relay.Package{Messages: []*relay.Message{{Topic: "hold", Payloads: payload.Payloads{payload.NewPayload(lua.LString("go"), payload.Lua)}}}},
	}}
	output.Reset()
	if err := proc.Step(events, &output); err != nil {
		t.Fatal(err)
	}
	ephemeralRunUntilDone(t, proc)
}

// TestEphemeral_AbortStopsProducersWithoutChannelOps confirms Abort can run
// from a non-step goroutine: it bumps the epoch and calls producerStop
// without touching Lua channel state.
func TestEphemeral_AbortStopsProducersWithoutChannelOps(t *testing.T) {
	script := `
		local hold = channel.new(1)
		subscribe("hold", hold)
		hold:receive()
		return "ok"
	`
	proc := startEphemeralProcess(t, script)
	defer proc.Close()
	ephemeralRunUntilIdle(t, proc)

	var stopCount int32
	stop := func() { atomic.AddInt32(&stopCount, 1) }
	ch := NewChannel(2)
	chID, epoch := proc.RegisterEphemeral(ch, nil, stop, OverflowDrop)

	proc.Abort()

	if got := atomic.LoadInt32(&stopCount); got != 1 {
		t.Fatalf("Abort should invoke producerStop once, got %d", got)
	}
	// Channel is untouched.
	if ch.IsClosed() {
		t.Fatal("Abort must not close channels")
	}
	// Entry survives until the owner thread drains. Producers see frames
	// dropped via epoch mismatch.
	if proc.router.size() != 1 {
		t.Fatalf("Abort should not clear router entries, got size=%d", proc.router.size())
	}

	// A frame with the old epoch is dropped silently.
	sendEphemeralFrame(t, proc, &EphemeralFrame{
		Epoch:    epoch,
		ChID:     chID,
		Gen:      0,
		HasValue: true,
		Payloads: payload.Payloads{payload.NewPayload(lua.LString("stale-after-abort"), payload.Lua)},
	})
	if ch.buffer.Len() != 0 {
		t.Fatal("frame with pre-Abort epoch must be dropped")
	}

	// drainEphemeralChannels (called via Close) tears the rest down. The
	// stop closure is sync.Once so the count stays at 1.
	proc.drainEphemeralChannels()
	if got := atomic.LoadInt32(&stopCount); got != 1 {
		t.Fatalf("subsequent drain must not re-stop, got %d", got)
	}
	if !ch.IsClosed() {
		t.Error("drainEphemeralChannels should close registered channels")
	}

	// Release the script.
	var output process.StepOutput
	events := []process.Event{{
		Type: process.EventMessage,
		Data: &relay.Package{Messages: []*relay.Message{{Topic: "hold", Payloads: payload.Payloads{payload.NewPayload(lua.LString("go"), payload.Lua)}}}},
	}}
	output.Reset()
	if err := proc.Step(events, &output); err != nil {
		t.Fatal(err)
	}
	ephemeralRunUntilDone(t, proc)
}

// TestEphemeral_OverflowDrop_DoesNotGrowChannel: full buffer, no waiter,
// new frame arrives. With OverflowDrop the channel state is unchanged.
func TestEphemeral_OverflowDrop_DoesNotGrowChannel(t *testing.T) {
	script := `
		local hold = channel.new(1)
		subscribe("hold", hold)
		hold:receive()
		return "ok"
	`
	proc := startEphemeralProcess(t, script)
	defer proc.Close()
	ephemeralRunUntilIdle(t, proc)

	ch := NewChannel(1)
	ch.buffer.PushBack(lua.LString("seed"))
	chID, epoch := proc.RegisterEphemeral(ch, nil, nil, OverflowDrop)

	for i := 0; i < 100; i++ {
		sendEphemeralFrame(t, proc, &EphemeralFrame{
			Epoch: epoch, ChID: chID, Gen: 0, HasValue: true,
			Payloads: payload.Payloads{payload.NewPayload(lua.LString("flood"), payload.Lua)},
		})
	}

	if ch.buffer.Len() != 1 {
		t.Fatalf("OverflowDrop must keep buffer size, got %d", ch.buffer.Len())
	}
	if ch.sendq.Len() != 0 {
		t.Fatalf("OverflowDrop must not create phantom senders, sendq=%d", ch.sendq.Len())
	}
	if v := ch.buffer.Front().Value; v.(lua.LString) != "seed" {
		t.Errorf("OverflowDrop must keep the original buffered value, got %v", v)
	}

	// Release the script.
	var output process.StepOutput
	events := []process.Event{{
		Type: process.EventMessage,
		Data: &relay.Package{Messages: []*relay.Message{{Topic: "hold", Payloads: payload.Payloads{payload.NewPayload(lua.LString("go"), payload.Lua)}}}},
	}}
	output.Reset()
	if err := proc.Step(events, &output); err != nil {
		t.Fatal(err)
	}
	ephemeralRunUntilDone(t, proc)
}

// TestEphemeral_OverflowCoalesce_ReplacesOldest: full buffer + new frame
// pops the oldest and pushes the new one.
func TestEphemeral_OverflowCoalesce_ReplacesOldest(t *testing.T) {
	script := `
		local hold = channel.new(1)
		subscribe("hold", hold)
		hold:receive()
		return "ok"
	`
	proc := startEphemeralProcess(t, script)
	defer proc.Close()
	ephemeralRunUntilIdle(t, proc)

	ch := NewChannel(1)
	ch.buffer.PushBack(lua.LString("seed"))
	chID, epoch := proc.RegisterEphemeral(ch, nil, nil, OverflowCoalesce)

	sendEphemeralFrame(t, proc, &EphemeralFrame{
		Epoch: epoch, ChID: chID, Gen: 0, HasValue: true,
		Payloads: payload.Payloads{payload.NewPayload(lua.LString("newest"), payload.Lua)},
	})

	if ch.buffer.Len() != 1 {
		t.Fatalf("buffer should stay at cap=1, got %d", ch.buffer.Len())
	}
	if v := ch.buffer.Front().Value; v.(lua.LString) != "newest" {
		t.Errorf("OverflowCoalesce should keep the newest value, got %v", v)
	}

	// Release the script.
	var output process.StepOutput
	events := []process.Event{{
		Type: process.EventMessage,
		Data: &relay.Package{Messages: []*relay.Message{{Topic: "hold", Payloads: payload.Payloads{payload.NewPayload(lua.LString("go"), payload.Lua)}}}},
	}}
	output.Reset()
	if err := proc.Step(events, &output); err != nil {
		t.Fatal(err)
	}
	ephemeralRunUntilDone(t, proc)
}

// TestEphemeral_OverflowClose_StopsProducerAndClosesChannel: full buffer
// triggers OverflowClose; the entry is removed, producerStop runs, and
// the Lua channel is closed.
func TestEphemeral_OverflowClose_StopsProducerAndClosesChannel(t *testing.T) {
	script := `
		local hold = channel.new(1)
		subscribe("hold", hold)
		hold:receive()
		return "ok"
	`
	proc := startEphemeralProcess(t, script)
	defer proc.Close()
	ephemeralRunUntilIdle(t, proc)

	var stopCount int32
	ch := NewChannel(1)
	ch.buffer.PushBack(lua.LString("seed"))
	chID, epoch := proc.RegisterEphemeral(ch, nil, func() { atomic.AddInt32(&stopCount, 1) }, OverflowClose)

	sendEphemeralFrame(t, proc, &EphemeralFrame{
		Epoch: epoch, ChID: chID, Gen: 0, HasValue: true,
		Payloads: payload.Payloads{payload.NewPayload(lua.LString("over"), payload.Lua)},
	})

	if atomic.LoadInt32(&stopCount) != 1 {
		t.Errorf("OverflowClose should call producerStop exactly once, got %d", atomic.LoadInt32(&stopCount))
	}
	if !ch.IsClosed() {
		t.Error("OverflowClose should close the channel")
	}
	if proc.router.size() != 0 {
		t.Errorf("OverflowClose should remove the entry, router size=%d", proc.router.size())
	}

	// Release the script.
	var output process.StepOutput
	events := []process.Event{{
		Type: process.EventMessage,
		Data: &relay.Package{Messages: []*relay.Message{{Topic: "hold", Payloads: payload.Payloads{payload.NewPayload(lua.LString("go"), payload.Lua)}}}},
	}}
	output.Reset()
	if err := proc.Step(events, &output); err != nil {
		t.Fatal(err)
	}
	ephemeralRunUntilDone(t, proc)
}

// TestEphemeral_InitDrainsPriorRouter: pool reuse path. After Init the
// epoch must be higher than any frame produced under the previous
// incarnation, and the producerStop closures must have fired.
func TestEphemeral_InitDrainsPriorRouter(t *testing.T) {
	proto, err := lua.CompileString(`return "first"`, "first.lua")
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

	// Register a fake ephemeral against this incarnation.
	var stopCount int32
	ch := NewChannel(1)
	_, oldEpoch := proc.RegisterEphemeral(ch, nil, func() { atomic.AddInt32(&stopCount, 1) }, OverflowDrop)

	// Pool reuse: a second Init must invalidate the prior entry.
	ctx2, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx2, "", nil); err != nil {
		t.Fatal(err)
	}

	if atomic.LoadInt32(&stopCount) != 1 {
		t.Errorf("Init must drain previous router, expected stop=1 got %d", atomic.LoadInt32(&stopCount))
	}
	if got := proc.epoch.Load(); got <= oldEpoch {
		t.Errorf("epoch must be strictly greater after Init, was %d now %d", oldEpoch, got)
	}
	if proc.router != nil && proc.router.size() != 0 {
		t.Errorf("router entries must be cleared after Init drain, got %d", proc.router.size())
	}
}

// TestEphemeral_ConvertNilSuppressesDelivery: a converter that returns nil
// must not push anything to the channel.
func TestEphemeral_ConvertNilSuppressesDelivery(t *testing.T) {
	script := `
		local hold = channel.new(1)
		subscribe("hold", hold)
		hold:receive()
		return "ok"
	`
	proc := startEphemeralProcess(t, script)
	defer proc.Close()
	ephemeralRunUntilIdle(t, proc)

	ch := NewChannel(2)
	chID, epoch := proc.RegisterEphemeral(ch, func(_ context.Context, _ *lua.LState, _ pid.PID, _ []payload.Payload) lua.LValue {
		return nil
	}, nil, OverflowDrop)

	sendEphemeralFrame(t, proc, &EphemeralFrame{
		Epoch: epoch, ChID: chID, Gen: 0, HasValue: true,
		Payloads: payload.Payloads{payload.NewPayload(lua.LString("ignored"), payload.Lua)},
	})

	if ch.buffer.Len() != 0 {
		t.Fatal("nil from converter must suppress channel delivery")
	}

	// Release the script.
	var output process.StepOutput
	events := []process.Event{{
		Type: process.EventMessage,
		Data: &relay.Package{Messages: []*relay.Message{{Topic: "hold", Payloads: payload.Payloads{payload.NewPayload(lua.LString("go"), payload.Lua)}}}},
	}}
	output.Reset()
	if err := proc.Step(events, &output); err != nil {
		t.Fatal(err)
	}
	ephemeralRunUntilDone(t, proc)
}

// TestEphemeral_ScalesUnderLongRunning is the scheduler/job_worker shape.
// Many sequential register+deliver+close cycles must keep map sizes flat.
func TestEphemeral_ScalesUnderLongRunning(t *testing.T) {
	script := `
		local hold = channel.new(1)
		subscribe("hold", hold)
		hold:receive()
		return "ok"
	`
	proc := startEphemeralProcess(t, script)
	defer proc.Close()
	ephemeralRunUntilIdle(t, proc)

	const N = 5000
	for i := 0; i < N; i++ {
		ch := NewChannel(1)
		chID, epoch := proc.RegisterEphemeral(ch, nil, nil, OverflowDrop)
		sendEphemeralFrame(t, proc, &EphemeralFrame{
			Epoch: epoch, ChID: chID, Gen: 0, HasValue: true, Close: true,
			Payloads: payload.Payloads{payload.NewPayload(lua.LString("tick"), payload.Lua)},
		})
		if !ch.IsClosed() {
			t.Fatalf("iter %d: channel should be closed after HasValue+Close frame", i)
		}
	}

	if got := proc.router.size(); got != 0 {
		t.Fatalf("router map must be empty after %d register/close cycles, got %d", N, got)
	}

	// Subscription maps and handlers untouched by the router.
	proc.subs.mu.RLock()
	subsLen := len(proc.subs.byTopic)
	proc.subs.mu.RUnlock()
	if subsLen != 1 {
		t.Errorf("subs.byTopic should hold only 'hold' (len=1), got %d", subsLen)
	}
	if len(proc.handlers) != 0 {
		t.Errorf("handlers map should be empty, got %d entries", len(proc.handlers))
	}

	// Release the script.
	var output process.StepOutput
	events := []process.Event{{
		Type: process.EventMessage,
		Data: &relay.Package{Messages: []*relay.Message{{Topic: "hold", Payloads: payload.Payloads{payload.NewPayload(lua.LString("go"), payload.Lua)}}}},
	}}
	output.Reset()
	if err := proc.Step(events, &output); err != nil {
		t.Fatal(err)
	}
	ephemeralRunUntilDone(t, proc)
}
