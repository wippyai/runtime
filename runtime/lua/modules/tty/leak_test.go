// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/runtime/lua/engine"
	svcterm "github.com/wippyai/runtime/service/terminal"
)

// The tty events subscription is engine-level: tty.events() runs purely in Go
// (no yield), calling Process.SubscribeExisting(@tty/events, ch) +
// SetTopicHandler(eventHandler). The producer is the terminal InputReader's
// readLoop, which delivers payload.New(*TTYEvent) packages on @tty/events via
// the scheduler. These tests drive a real engine.Process through Step and
// deliver TTYEvent packages in exactly that producer shape, so delivery is
// real (the asserted Lua values are genuine event tables), and they prove the
// production removal paths -- terminal/close delivery via deliverMessage and
// drainSubscriptionChannels on process teardown -- reclaim the subscription
// without accumulating in subs.byTopic / subs.byChannel / handlers.

func startTTYProcess(t *testing.T, script string) (*engine.Process, pid.PID) {
	t.Helper()

	proto, err := lua.CompileString(script, "tty_leak.lua")
	require.NoError(t, err)

	proc, err := engine.NewProcess(
		engine.WithProto(proto),
		engine.WithModuleBinder(func(l *lua.LState) error {
			engine.LoadModuleDef(l, engine.ChannelModule)
			return nil
		}),
		engine.WithModuleBinder(func(l *lua.LState) error {
			tbl, _ := Module.Build()
			l.SetGlobal(Module.Name, tbl)
			return nil
		}),
	)
	require.NoError(t, err)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	rawPID := pid.PID{Host: "tty.leak.test", UniqID: "tty-leak"}
	p := rawPID.Precomputed()
	require.NoError(t, runtime.SetFramePID(ctx, p))
	require.NoError(t, proc.Init(ctx, "", nil))

	return proc, p
}

func ttyRunUntilIdle(t *testing.T, proc *engine.Process) {
	t.Helper()
	var output process.StepOutput
	const maxSteps = 100
	for i := 0; i < maxSteps; i++ {
		output.Reset()
		require.NoError(t, proc.Step(nil, &output))
		if output.Status() == process.StepIdle || output.Status() == process.StepDone {
			return
		}
	}
	t.Fatalf("did not reach idle in %d steps", maxSteps)
}

func ttyRunUntilDone(t *testing.T, proc *engine.Process) {
	t.Helper()
	var output process.StepOutput
	const maxSteps = 200
	for i := 0; i < maxSteps; i++ {
		output.Reset()
		require.NoError(t, proc.Step(nil, &output))
		if output.Status() == process.StepDone {
			return
		}
	}
	t.Fatalf("did not reach done in %d steps", maxSteps)
}

// ttyDeliverEvent mirrors InputReader.sendEvent: a relay package targeting the
// process with a single payload.New(*TTYEvent) on the @tty/events topic.
func ttyDeliverEvent(t *testing.T, proc *engine.Process, ev *svcterm.TTYEvent) {
	t.Helper()
	var output process.StepOutput
	events := []process.Event{{
		Type: process.EventMessage,
		Data: &relay.Package{
			Messages: []*relay.Message{{
				Topic:    svcterm.TopicTTYEvents,
				Payloads: payload.Payloads{payload.New(ev)},
			}},
		},
	}}
	output.Reset()
	require.NoError(t, proc.Step(events, &output))
}

func ttyHandlerCount(proc *engine.Process) int {
	_, hasHandler := proc.GetTopicHandler(svcterm.TopicTTYEvents)
	if hasHandler {
		return 1
	}
	return 0
}

// A single subscription receives real terminal input events delivered in the
// production shape and converts them to Lua event tables. Proves the producer
// path is real (non-vacuous): the script asserts the received event is the key
// it was sent.
func TestTTYEventsRealDelivery(t *testing.T) {
	script := `
		local ch, err = tty.events()
		if err then return nil, "events failed: " .. tostring(err) end

		local ev, ok = ch:receive()
		if not ok then return nil, "channel closed" end
		if ev == nil then return nil, "nil event" end
		if ev.type ~= "key" then return nil, "wrong type: " .. tostring(ev.type) end
		if ev.key ~= "a" then return nil, "wrong key: " .. tostring(ev.key) end
		return "ok"
	`
	proc, _ := startTTYProcess(t, script)
	defer proc.Close()

	ttyRunUntilIdle(t, proc)

	require.Equal(t, 1, proc.LiveSubscriptionCount(), "tty.events must register exactly one subscription")
	require.Equal(t, 1, ttyHandlerCount(proc), "tty.events must register a topic handler")

	ttyDeliverEvent(t, proc, &svcterm.TTYEvent{Type: "key", Key: "a", KeyType: "runes", Action: "press"})
	ttyRunUntilDone(t, proc)

	require.Equal(t, 0, proc.LiveSubscriptionCount(), "subscription must be reclaimed after the process completes")
}

// A single long-lived actor subscribes to tty events, receives a real event,
// then the stream is closed by a terminal frame (the engine reclaim path for
// any subscription), and re-subscribes -- HUNDREDS of times. Because
// tty.events() enforces one channel per @tty/events topic, a leak would make
// the second subscribe fail; instead each terminal reclaim frees the topic so
// the next subscribe succeeds. The live subscription count, the byChannel map,
// and the handler map must stay bounded at one and never accumulate.
func TestTTYEventsNoSubscriptionAccumulation(t *testing.T) {
	const cycles = 300

	// The actor loops: subscribe, receive one real key event, observe the
	// terminal close, repeat. Each terminal reclaim returns (nil,false) from
	// receive, signaling the loop to re-subscribe.
	script := fmt.Sprintf(`
		local n = %d
		local got = 0
		for i = 1, n do
			local ch, err = tty.events()
			if err then return nil, "events failed at " .. i .. ": " .. tostring(err) end

			local ev, ok = ch:receive()
			if not ok then return nil, "channel closed before event at " .. i end
			if ev == nil or ev.type ~= "key" then return nil, "bad event at " .. i end
			got = got + 1

			-- Next receive observes the terminal close that reclaims the sub.
			local _, ok2 = ch:receive()
			if ok2 then return nil, "expected terminal close at " .. i end
		end
		return got
	`, cycles)
	proto, err := lua.CompileString(script, "tty_loop.lua")
	require.NoError(t, err)

	proc, err := engine.NewProcess(
		engine.WithProto(proto),
		engine.WithModuleBinder(func(l *lua.LState) error {
			engine.LoadModuleDef(l, engine.ChannelModule)
			return nil
		}),
		engine.WithModuleBinder(func(l *lua.LState) error {
			tbl, _ := Module.Build()
			l.SetGlobal(Module.Name, tbl)
			return nil
		}),
	)
	require.NoError(t, err)
	defer proc.Close()

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	rawPID := pid.PID{Host: "tty.leak.test", UniqID: "tty-loop"}
	p := rawPID.Precomputed()
	require.NoError(t, runtime.SetFramePID(ctx, p))
	require.NoError(t, proc.Init(ctx, "", nil))

	maxLive := 0
	maxHandlers := 0

	sample := func() {
		if live := proc.LiveSubscriptionCount(); live > maxLive {
			maxLive = live
		}
		if h := ttyHandlerCount(proc); h > maxHandlers {
			maxHandlers = h
		}
	}

	var output process.StepOutput
	// Run the first script step so it subscribes and parks on receive.
	for i := 0; i < 50; i++ {
		output.Reset()
		require.NoError(t, proc.Step(nil, &output))
		if output.Status() == process.StepIdle || output.Status() == process.StepDone {
			break
		}
	}
	require.Equal(t, process.StepIdle, output.Status(), "actor must park on first receive")

	for i := 0; i < cycles; i++ {
		sample()
		// Deliver a real key event; eventHandler converts it to a Lua table
		// the script consumes.
		output.Reset()
		require.NoError(t, proc.Step([]process.Event{{
			Type: process.EventMessage,
			Data: &relay.Package{Messages: []*relay.Message{{
				Topic:    svcterm.TopicTTYEvents,
				Payloads: payload.Payloads{payload.New(&svcterm.TTYEvent{Type: "key", Key: "x", KeyType: "runes", Action: "press"})},
			}}},
		}}, &output))

		sample()
		// Terminal frame reclaims the subscription; the actor re-subscribes.
		output.Reset()
		require.NoError(t, proc.Step([]process.Event{{
			Type: process.EventMessage,
			Data: &relay.Package{Messages: []*relay.Message{{
				Topic:    svcterm.TopicTTYEvents,
				Payloads: payload.Payloads{payload.NewTerminal()},
			}}},
		}}, &output))

		// Drive the actor back to its next parked receive (or completion).
		for j := 0; j < 50; j++ {
			if output.Status() == process.StepIdle || output.Status() == process.StepDone {
				break
			}
			output.Reset()
			require.NoError(t, proc.Step(nil, &output))
		}
	}

	// Drain to completion.
	for j := 0; j < 200 && output.Status() != process.StepDone; j++ {
		output.Reset()
		require.NoError(t, proc.Step(nil, &output))
	}
	require.Equal(t, process.StepDone, output.Status(), "actor did not complete")

	res := output.Result()
	require.NotNil(t, res)
	require.Equal(t, int64(cycles), toInt64(res), "every cycle must receive a real key event")

	t.Logf("tty events subscribe/close loop: %d cycles, max live subscriptions=%d, max handlers=%d", cycles, maxLive, maxHandlers)

	assert.LessOrEqual(t, maxLive, 1, "tty subscriptions accumulated (should stay 1 per topic)")
	assert.LessOrEqual(t, maxHandlers, 1, "topic handlers accumulated across cycles")
	assert.Equal(t, 0, proc.LiveSubscriptionCount(), "subscription survived final reclaim")
}

// The production drain path (drainSubscriptionChannels, run on Init /
// clearExecution / Close) must remove a live tty events subscription and its
// handler when the process tears down without an explicit close -- the actor
// holds an open stream and is terminated.
func TestTTYEventsDrainRemovesSubscription(t *testing.T) {
	script := `
		local ch, err = tty.events()
		if err then return nil, "events failed: " .. tostring(err) end
		-- Park forever on the open stream; never close it.
		ch:receive()
		return "ok"
	`
	proc, _ := startTTYProcess(t, script)

	ttyRunUntilIdle(t, proc)

	require.Equal(t, 1, proc.LiveSubscriptionCount(), "precondition: one live tty subscription")
	require.Equal(t, 1, ttyHandlerCount(proc), "precondition: tty topic handler installed")

	// Close runs drainSubscriptionChannels on the production teardown path.
	proc.Close()

	assert.Equal(t, 0, proc.LiveSubscriptionCount(), "drain did not remove the tty subscription")
	assert.Equal(t, 0, ttyHandlerCount(proc), "drain did not remove the tty topic handler")
}

func toInt64(p payload.Payload) int64 {
	if p == nil {
		return 0
	}
	switch v := p.Data().(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case lua.LInteger:
		return int64(v)
	case lua.LNumber:
		return int64(v)
	}
	return 0
}
