// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"testing"

	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
)

// Regression coverage for three correctness bugs in the channel result and
// subscription cleanup path:
//
// 1. processSubscribeYields unsubscribe path never removes the topic handler.
// 2. processSubscribeYields unsubscribe path discards Channel.Close result,
//    leaving blocked receivers hung forever.
// 3. deliverMessage uses Channel.Send(nil,...) which on a full channel pushes
//    a phantom blocked-sender (task=nil) into sendq, leaking memory under
//    producer pressure.

func startCleanupProcess(t *testing.T, script string) *Process {
	t.Helper()

	proto, err := lua.CompileString(script, "cleanup.lua")
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

func cleanupRunUntilIdle(t *testing.T, proc *Process) {
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

func cleanupRunUntilDone(t *testing.T, proc *Process) {
	t.Helper()
	var output process.StepOutput
	const maxSteps = 100
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

func cleanupSendMessage(t *testing.T, proc *Process, topic string, payloads payload.Payloads) {
	t.Helper()
	var output process.StepOutput
	events := []process.Event{{
		Type: process.EventMessage,
		Data: &relay.Package{
			Messages: []*relay.Message{{Topic: topic, Payloads: payloads}},
		},
	}}
	output.Reset()
	if err := proc.Step(events, &output); err != nil {
		t.Fatalf("send to %q failed: %v", topic, err)
	}
}

// passthroughHandler returns the first string payload as-is; used to install
// a topic handler so we can verify unsubscribe removes it.
func passthroughHandler(_ context.Context, _ *lua.LState, _ pid.PID, _ string, payloads []payload.Payload) lua.LValue {
	if len(payloads) > 0 {
		if s, ok := payloads[0].Data().(lua.LString); ok {
			return s
		}
	}
	return lua.LNil
}

// Unsubscribe must remove the matching topic handler. Today
// processSubscribeYields only clears subs.byTopic / subs.byChannel; the
// handler entry is leaked across listen/unlisten cycles.
//
// We pause execution after unsubscribe so the assertion runs before
// clearExecution() wipes the handlers map at StepDone.
func TestUnsubscribe_RemovesTopicHandler(t *testing.T) {
	script := `
		local ch = channel.new(1)
		local control = channel.new(1)
		subscribe("with_handler", ch)
		subscribe("control", control)

		control:receive()
		unsubscribe(ch)

		-- Re-block so the test can inspect proc.handlers BEFORE the
		-- process completes (which would clear it via clearExecution).
		local hold = channel.new(1)
		subscribe("hold", hold)
		hold:receive()

		return "ok"
	`
	proc := startCleanupProcess(t, script)
	defer proc.Close()

	cleanupRunUntilIdle(t, proc)

	// Plain Lua subscribe does not register a handler. The leak only
	// manifests when a Go-side module (time, websocket, etc.) installs one.
	proc.SetTopicHandler("with_handler", passthroughHandler)
	if _, ok := proc.GetTopicHandler("with_handler"); !ok {
		t.Fatal("precondition: handler should be installed")
	}

	cleanupSendMessage(t, proc, "control", payload.Payloads{payload.NewPayload(lua.LString("go"), payload.Lua)})
	cleanupRunUntilIdle(t, proc)

	proc.subs.mu.RLock()
	_, subStillRegistered := proc.subs.byTopic["with_handler"]
	proc.subs.mu.RUnlock()
	if subStillRegistered {
		t.Fatal("subs.byTopic still contains 'with_handler' after unsubscribe")
	}

	if _, ok := proc.GetTopicHandler("with_handler"); ok {
		t.Fatal("proc.handlers still contains 'with_handler' after unsubscribe (handler leak)")
	}

	// Let the process complete cleanly.
	cleanupSendMessage(t, proc, "hold", payload.Payloads{payload.NewPayload(lua.LString("go"), payload.Lua)})
	cleanupRunUntilDone(t, proc)
}

// Unsubscribe closes the channel; blocked receivers must wake with (nil,
// false). Today processSubscribeYields discards the Channel.Close result.
func TestUnsubscribe_WakesBlockedReceivers(t *testing.T) {
	script := `
		local ch = channel.new(0)
		subscribe("subbed", ch)

		local results = {}
		for i = 1, 3 do
			coroutine.spawn(function()
				local v, ok = ch:receive()
				table.insert(results, tostring(ok))
			end)
		end

		-- Let the spawned coroutines reach their blocked receive.
		for i = 1, 5 do coroutine.yield() end

		unsubscribe(ch)

		-- Give the runtime room to wake the receivers.
		for i = 1, 10 do coroutine.yield() end

		if #results ~= 3 then
			return nil, "expected 3 wake results, got "..#results
		end
		for _, r in ipairs(results) do
			if r ~= "false" then
				return nil, "expected ok=false for woken receiver, got "..r
			end
		end
		return "ok"
	`
	proc := startCleanupProcess(t, script)
	defer proc.Close()

	cleanupRunUntilDone(t, proc)
}

// A terminal payload arriving on a subscribed topic closes the channel; any
// blocked receivers must wake. Same bug shape as the unsubscribe case but
// inside deliverMessage.
func TestDeliverMessage_TerminalCloseWakesBlockedReceiver(t *testing.T) {
	script := `
		local ch = channel.new(0)
		subscribe("terminal_topic", ch)

		local v, ok = ch:receive()
		if v ~= nil then
			return nil, "expected nil value on terminal close, got "..tostring(v)
		end
		if ok ~= false then
			return nil, "expected ok=false on terminal close, got "..tostring(ok)
		end
		return "ok"
	`
	proc := startCleanupProcess(t, script)
	defer proc.Close()

	cleanupRunUntilIdle(t, proc)

	cleanupSendMessage(t, proc, "terminal_topic", payload.Payloads{payload.NewTerminal()})
	cleanupRunUntilDone(t, proc)
}

// External delivery into a full subscription channel previously called
// Channel.Send(nil, ...) which pushed a chanOp{task: nil} into sendq, leaking
// memory under producer pressure. With the CanSend guard the channel's
// sendq must remain empty.
func TestDeliverMessage_FullChannelDoesNotPhantomSend(t *testing.T) {
	script := `
		local ch = channel.new(1)
		subscribe("burst", ch)
		-- Fill the cap-1 buffer locally so external sends will overflow.
		ch:send("seed")
		-- Pause while the Go test pumps messages into the full buffer.
		local control = channel.new(1)
		subscribe("control", control)
		control:receive()
		return "ok"
	`
	proc := startCleanupProcess(t, script)
	defer proc.Close()

	cleanupRunUntilIdle(t, proc)

	proc.subs.mu.RLock()
	sub, ok := proc.subs.byTopic["burst"]
	proc.subs.mu.RUnlock()
	if !ok {
		t.Fatal("precondition: 'burst' subscription missing")
	}
	subbedCh := sub.channel

	for i := 0; i < 50; i++ {
		cleanupSendMessage(t, proc, "burst", payload.Payloads{payload.NewPayload(lua.LString("x"), payload.Lua)})
	}

	if got := subbedCh.sendq.Len(); got != 0 {
		t.Fatalf("phantom blocked senders accumulated: sendq.Len()=%d, want 0", got)
	}

	cleanupSendMessage(t, proc, "control", payload.Payloads{payload.NewPayload(lua.LString("go"), payload.Lua)})
	cleanupRunUntilDone(t, proc)
}
