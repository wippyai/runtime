package engine

import (
	"context"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	lua "github.com/yuin/gopher-lua"
)

// Helper to send a message event with payload to a process
func sendMessageWithPayload(proc *Process, topic string, payloads payload.Payloads, output *process.StepOutput) error {
	events := []process.Event{{
		Type: process.EventMessage,
		Data: &relay.Package{
			Messages: []*relay.Message{{Topic: topic, Payloads: payloads}},
		},
	}}
	output.Reset()
	return proc.Step(events, output)
}

// startEventProcess creates a process with subscribe/unsubscribe bindings.
func startEventProcess(t *testing.T, script string) *Process {
	t.Helper()

	proto, _ := lua.CompileString(script, "test.lua")
	proc := NewProcess(
		WithProto(proto),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	BindChannelFunctions(proc.State())
	BindSubscribeFunctions(proc.State())

	return proc
}

// runEventUntilIdle runs the process until it's idle (waiting for messages).
func runEventUntilIdle(t *testing.T, proc *Process, maxSteps int) error {
	t.Helper()
	var output process.StepOutput
	for i := 0; i < maxSteps; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			return err
		}
		if output.Status() == process.StepIdle {
			return nil
		}
		if output.Status() == process.StepDone {
			return nil
		}
	}
	t.Fatalf("did not reach idle in %d steps", maxSteps)
	return nil
}

// runEventUntilDone runs the process until it completes.
func runEventUntilDone(t *testing.T, proc *Process, maxSteps int) error {
	t.Helper()
	var output process.StepOutput
	for i := 0; i < maxSteps; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			return err
		}
		if output.Status() == process.StepDone {
			return nil
		}
	}
	t.Fatalf("did not reach done in %d steps", maxSteps)
	return nil
}

// TestEventMessageWithStringPayload tests that string payloads are correctly
// delivered to Lua via EventMessage.
func TestEventMessageWithStringPayload(t *testing.T) {
	script := `
		local inbox = channel.new(10)
		subscribe("test_topic", inbox)
		local msg = inbox:receive()
		if msg ~= "hello world" then
			return nil, "expected 'hello world', got: " .. tostring(msg)
		end
		return "success"
	`

	proc := startEventProcess(t, script)
	defer proc.Close()

	if err := runEventUntilIdle(t, proc, 20); err != nil {
		t.Fatal(err)
	}

	if !proc.HasSubscriptions() {
		t.Error("expected active subscription")
	}

	// Send message with string payload
	var output process.StepOutput
	stringPayload := payload.NewString("hello world")
	if err := sendMessageWithPayload(proc, "test_topic", payload.Payloads{stringPayload}, &output); err != nil {
		t.Fatal(err)
	}

	if err := runEventUntilDone(t, proc, 20); err != nil {
		t.Fatalf("Process failed: %v", err)
	}
}

// TestEventMessageWithTablePayload tests that Lua table payloads are correctly
// delivered to Lua via EventMessage using payload.Lua format.
// Note: JSON format requires a transcoder to be set in context.
func TestEventMessageWithTablePayload(t *testing.T) {
	script := `
		local inbox = channel.new(10)
		subscribe("data_topic", inbox)
		local msg = inbox:receive()

		if type(msg) ~= "table" then
			return nil, "expected table, got: " .. type(msg)
		end
		if msg.name ~= "test" then
			return nil, "expected name='test', got: " .. tostring(msg.name)
		end
		if msg.count ~= 42 then
			return nil, "expected count=42, got: " .. tostring(msg.count)
		end
		return "success"
	`

	proto, _ := lua.CompileString(script, "test.lua")
	proc := NewProcess(WithProto(proto))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())

	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer proc.Close()

	BindChannelFunctions(proc.State())
	BindSubscribeFunctions(proc.State())

	if err := runEventUntilIdle(t, proc, 20); err != nil {
		t.Fatal(err)
	}

	// Create Lua table directly and send as Lua payload
	tbl := proc.State().CreateTable(0, 2)
	tbl.RawSetString("name", lua.LString("test"))
	tbl.RawSetString("count", lua.LNumber(42))

	var output process.StepOutput
	luaTablePayload := payload.NewPayload(tbl, payload.Lua)
	if err := sendMessageWithPayload(proc, "data_topic", payload.Payloads{luaTablePayload}, &output); err != nil {
		t.Fatal(err)
	}

	if err := runEventUntilDone(t, proc, 20); err != nil {
		t.Fatalf("Process failed: %v", err)
	}
}

// TestEventMessageMultiplePayloads tests that multiple payloads in a single
// message are correctly delivered.
func TestEventMessageMultiplePayloads(t *testing.T) {
	script := `
		local inbox = channel.new(10)
		subscribe("multi_topic", inbox)
		local msg = inbox:receive()

		if type(msg) ~= "table" then
			return nil, "expected table (array), got: " .. type(msg)
		end
		if #msg ~= 2 then
			return nil, "expected 2 items, got: " .. #msg
		end
		if msg[1] ~= "first" then
			return nil, "expected first='first', got: " .. tostring(msg[1])
		end
		if msg[2] ~= "second" then
			return nil, "expected second='second', got: " .. tostring(msg[2])
		end
		return "success"
	`

	proc := startEventProcess(t, script)
	defer proc.Close()

	if err := runEventUntilIdle(t, proc, 20); err != nil {
		t.Fatal(err)
	}

	// Send message with multiple payloads
	var output process.StepOutput
	payloads := payload.Payloads{
		payload.NewString("first"),
		payload.NewString("second"),
	}
	if err := sendMessageWithPayload(proc, "multi_topic", payloads, &output); err != nil {
		t.Fatal(err)
	}

	if err := runEventUntilDone(t, proc, 20); err != nil {
		t.Fatalf("Process failed: %v", err)
	}
}

// TestEventMessageBlockedReceiver tests that a blocked receiver wakes up
// when a message arrives.
func TestEventMessageBlockedReceiver(t *testing.T) {
	script := `
		local inbox = channel.new(0)  -- unbuffered channel
		subscribe("wake_topic", inbox)

		-- This will block since channel is empty
		local msg = inbox:receive()
		return msg
	`

	proc := startEventProcess(t, script)
	defer proc.Close()

	if err := runEventUntilIdle(t, proc, 20); err != nil {
		t.Fatal(err)
	}

	// Process is now blocked on receive, send message to wake it
	var output process.StepOutput
	stringPayload := payload.NewString("wake up!")
	if err := sendMessageWithPayload(proc, "wake_topic", payload.Payloads{stringPayload}, &output); err != nil {
		t.Fatal(err)
	}

	if err := runEventUntilDone(t, proc, 20); err != nil {
		t.Fatalf("Process failed to complete after receiving message: %v", err)
	}
}

// TestEventMessageRoutingByTopic tests that messages are routed to the
// correct subscription by topic.
func TestEventMessageRoutingByTopic(t *testing.T) {
	script := `
		local inbox1 = channel.new(10)
		local inbox2 = channel.new(10)
		subscribe("topic1", inbox1)
		subscribe("topic2", inbox2)

		-- Wait for both messages
		coroutine.yield()  -- let messages arrive
		coroutine.yield()

		local msg1 = inbox1:receive()
		local msg2 = inbox2:receive()

		if msg1 ~= "for topic1" then
			return nil, "topic1 got wrong message: " .. tostring(msg1)
		end
		if msg2 ~= "for topic2" then
			return nil, "topic2 got wrong message: " .. tostring(msg2)
		end
		return "success"
	`

	proc := startEventProcess(t, script)
	defer proc.Close()

	if err := runEventUntilIdle(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	// Send messages to different topics
	var output process.StepOutput
	if err := sendMessageWithPayload(proc, "topic1", payload.Payloads{payload.NewString("for topic1")}, &output); err != nil {
		t.Fatal(err)
	}
	if err := sendMessageWithPayload(proc, "topic2", payload.Payloads{payload.NewString("for topic2")}, &output); err != nil {
		t.Fatal(err)
	}

	if err := runEventUntilDone(t, proc, 30); err != nil {
		t.Fatalf("Process failed: %v", err)
	}
}

// TestEventMessageLuaPayload tests sending a Lua value directly as payload.
func TestEventMessageLuaPayload(t *testing.T) {
	script := `
		local inbox = channel.new(10)
		subscribe("lua_topic", inbox)
		local msg = inbox:receive()
		if type(msg) ~= "number" or msg ~= 42 then
			return nil, "expected number 42, got: " .. type(msg) .. " " .. tostring(msg)
		end
		return "success"
	`

	proc := startEventProcess(t, script)
	defer proc.Close()

	if err := runEventUntilIdle(t, proc, 20); err != nil {
		t.Fatal(err)
	}

	// Send message with Lua value payload
	var output process.StepOutput
	luaPayload := payload.NewPayload(lua.LNumber(42), payload.Lua)
	if err := sendMessageWithPayload(proc, "lua_topic", payload.Payloads{luaPayload}, &output); err != nil {
		t.Fatal(err)
	}

	if err := runEventUntilDone(t, proc, 20); err != nil {
		t.Fatalf("Process failed: %v", err)
	}
}

// TestEventMessageNoSubscriber tests that messages to unsubscribed topics
// are ignored (no crash).
func TestEventMessageNoSubscriber(t *testing.T) {
	script := `
		local inbox = channel.new(10)
		subscribe("subscribed_topic", inbox)

		-- Send a message to a topic we're NOT subscribed to
		coroutine.yield()  -- wait for potential message

		-- Should still be able to complete
		unsubscribe(inbox)
		return "success"
	`

	proc := startEventProcess(t, script)
	defer proc.Close()

	if err := runEventUntilIdle(t, proc, 20); err != nil {
		t.Fatal(err)
	}

	// Send message to a topic that has no subscriber
	var output process.StepOutput
	if err := sendMessageWithPayload(proc, "unsubscribed_topic", payload.Payloads{payload.NewString("ignored")}, &output); err != nil {
		t.Fatal(err)
	}

	if err := runEventUntilDone(t, proc, 30); err != nil {
		t.Fatalf("Process failed: %v", err)
	}
}
