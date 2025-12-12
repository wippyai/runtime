package engine

import (
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	luapayload "github.com/wippyai/runtime/runtime/lua/engine/payload"
	systempayload "github.com/wippyai/runtime/system/payload"
	lua "github.com/yuin/gopher-lua"
)

// createTestTranscoder creates a transcoder with Lua payload support
func createTestTranscoder() payload.Transcoder {
	transcoder := systempayload.NewTranscoder()
	luapayload.Register(transcoder)
	return transcoder
}

// Response Channel Pattern Tests
//
// These tests verify the response channel pattern used by gov client/service:
// 1. Client creates a response channel using process.listen()
// 2. Client sends request with respond_to field
// 3. Service processes request and sends reply to respond_to channel
// 4. Client receives reply on the response channel
//
// Expected behavior:
// - listen() creates a channel that receives messages on that topic
// - Messages sent to the listened topic are delivered to the channel
// - Multiple response channels can coexist

func startResponseTestProcess(t *testing.T, script string) *Process {
	t.Helper()

	proto, err := lua.CompileString(script, "test.lua")
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	proc := NewProcess(WithProto(proto))

	// Start with root context that has AppContext for transcoder storage
	rootCtx := ctxapi.NewRootContext()
	ctx, _ := ctxapi.OpenFrameContext(rootCtx)

	// Set up proper transcoder for payload conversion
	ctx = payload.WithTranscoder(ctx, createTestTranscoder())

	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}

	ChannelModule.Load(proc.State())
	loadPubSubGlobals(proc.State())

	return proc
}

func runResponseTestUntilIdle(t *testing.T, proc *Process, maxSteps int) error {
	t.Helper()
	var output process.StepOutput
	for i := 0; i < maxSteps; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			return err
		}
		if output.Status() == process.StepIdle || output.Status() == process.StepDone {
			return nil
		}
	}
	t.Fatalf("did not reach idle in %d steps", maxSteps)
	return nil
}

func runResponseTestUntilDone(t *testing.T, proc *Process, maxSteps int) error {
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

func sendResponseTestMessage(proc *Process, topic string, payloads payload.Payloads, output *process.StepOutput) error {
	events := []process.Event{{
		Type: process.EventMessage,
		Data: &relay.Package{
			Messages: []*relay.Message{{Topic: topic, Payloads: payloads}},
		},
	}}
	output.Reset()
	return proc.Step(events, output)
}

// TestResponseChannelBasic verifies that a response channel receives messages sent to it
func TestResponseChannelBasic(t *testing.T) {
	script := `
		local response_ch = channel.new(10)
		local control = channel.new(10)

		-- Subscribe to a unique response topic
		subscribe("test.response.12345", response_ch)
		subscribe("control", control)

		-- Wait for control signal
		control:receive()

		-- Check response channel
		local result = channel.select{response_ch:case_receive(), default=true}

		if result.default then
			return nil, "response channel should have a message"
		end

		if result.value ~= "response_data" then
			return nil, "expected 'response_data', got " .. tostring(result.value)
		end

		return "ok"
	`

	proc := startResponseTestProcess(t, script)
	defer proc.Close()

	if err := runResponseTestUntilIdle(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	var output process.StepOutput

	// Send response to the response channel
	if err := sendResponseTestMessage(proc, "test.response.12345", payload.Payloads{payload.NewString("response_data")}, &output); err != nil {
		t.Fatal(err)
	}

	// Signal to check
	if err := sendResponseTestMessage(proc, "control", payload.Payloads{payload.NewString("go")}, &output); err != nil {
		t.Fatal(err)
	}

	if err := runResponseTestUntilDone(t, proc, 50); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	t.Log("TestResponseChannelBasic passed")
}

// TestResponseChannelWithTablePayload verifies table payloads work on response channels
func TestResponseChannelWithTablePayload(t *testing.T) {
	script := `
		local response_ch = channel.new(10)
		local control = channel.new(10)

		subscribe("test.response.table", response_ch)
		subscribe("control", control)

		control:receive()

		local result = channel.select{response_ch:case_receive(), default=true}

		if result.default then
			return nil, "response channel should have a message"
		end

		-- The message should be a table
		if type(result.value) ~= "table" then
			return nil, "expected table, got " .. type(result.value)
		end

		if result.value.request_id ~= "req-001" then
			return nil, "expected request_id 'req-001', got " .. tostring(result.value.request_id)
		end

		if result.value.success ~= true then
			return nil, "expected success true"
		end

		return "ok"
	`

	proc := startResponseTestProcess(t, script)
	defer proc.Close()

	if err := runResponseTestUntilIdle(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	var output process.StepOutput

	// Send table payload as Go map (transcoder converts to Lua)
	responsePayload := payload.New(map[string]any{
		"request_id": "req-001",
		"success":    true,
		"result":     "processed data",
	})
	if err := sendResponseTestMessage(proc, "test.response.table", payload.Payloads{responsePayload}, &output); err != nil {
		t.Fatal(err)
	}

	if err := sendResponseTestMessage(proc, "control", payload.Payloads{payload.NewString("go")}, &output); err != nil {
		t.Fatal(err)
	}

	if err := runResponseTestUntilDone(t, proc, 50); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	t.Log("TestResponseChannelWithTablePayload passed")
}

// TestMultipleResponseChannels verifies multiple response channels can coexist
func TestMultipleResponseChannels(t *testing.T) {
	script := `
		local response1 = channel.new(10)
		local response2 = channel.new(10)
		local response3 = channel.new(10)
		local control = channel.new(10)

		subscribe("response.ch1", response1)
		subscribe("response.ch2", response2)
		subscribe("response.ch3", response3)
		subscribe("control", control)

		control:receive()

		local function drain(ch)
			local count = 0
			while true do
				local result = channel.select{ch:case_receive(), default=true}
				if result.default then break end
				count = count + 1
			end
			return count
		end

		local c1 = drain(response1)
		local c2 = drain(response2)
		local c3 = drain(response3)

		if c1 ~= 1 then return nil, "response1 should have 1 message, got " .. c1 end
		if c2 ~= 2 then return nil, "response2 should have 2 messages, got " .. c2 end
		if c3 ~= 0 then return nil, "response3 should have 0 messages, got " .. c3 end

		return "ok"
	`

	proc := startResponseTestProcess(t, script)
	defer proc.Close()

	if err := runResponseTestUntilIdle(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	var output process.StepOutput

	// Send to different response channels
	if err := sendResponseTestMessage(proc, "response.ch1", payload.Payloads{payload.NewString("r1")}, &output); err != nil {
		t.Fatal(err)
	}
	if err := sendResponseTestMessage(proc, "response.ch2", payload.Payloads{payload.NewString("r2a")}, &output); err != nil {
		t.Fatal(err)
	}
	if err := sendResponseTestMessage(proc, "response.ch2", payload.Payloads{payload.NewString("r2b")}, &output); err != nil {
		t.Fatal(err)
	}
	// Note: no message to response3

	if err := sendResponseTestMessage(proc, "control", payload.Payloads{payload.NewString("go")}, &output); err != nil {
		t.Fatal(err)
	}

	if err := runResponseTestUntilDone(t, proc, 50); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	t.Log("TestMultipleResponseChannels passed")
}

// TestResponseChannelDoesNotLeakToInbox verifies response channel messages don't go to inbox
func TestResponseChannelDoesNotLeakToInbox(t *testing.T) {
	script := `
		local inbox_ch = channel.new(10)
		local response_ch = channel.new(10)
		local control = channel.new(10)

		subscribe("@pid/inbox", inbox_ch)
		subscribe("my.response.topic", response_ch)
		subscribe("control", control)

		control:receive()

		local function drain(ch)
			local count = 0
			while true do
				local result = channel.select{ch:case_receive(), default=true}
				if result.default then break end
				count = count + 1
			end
			return count
		end

		local response_count = drain(response_ch)
		local inbox_count = drain(inbox_ch)

		if response_count ~= 1 then
			return nil, "response channel should have 1 message, got " .. response_count
		end
		if inbox_count ~= 0 then
			return nil, "inbox should NOT receive message sent to listened topic, got " .. inbox_count
		end

		return "ok"
	`

	proc := startResponseTestProcess(t, script)
	defer proc.Close()

	if err := runResponseTestUntilIdle(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	var output process.StepOutput

	// Send to response topic (should go to response channel, NOT inbox)
	if err := sendResponseTestMessage(proc, "my.response.topic", payload.Payloads{payload.NewString("response")}, &output); err != nil {
		t.Fatal(err)
	}

	if err := sendResponseTestMessage(proc, "control", payload.Payloads{payload.NewString("go")}, &output); err != nil {
		t.Fatal(err)
	}

	if err := runResponseTestUntilDone(t, proc, 50); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	t.Log("TestResponseChannelDoesNotLeakToInbox passed")
}

// TestResponseChannelBlockingReceive verifies blocking receive works on response channel
func TestResponseChannelBlockingReceive(t *testing.T) {
	script := `
		local response_ch = channel.new(10)
		subscribe("blocking.response", response_ch)

		-- This will block until message arrives
		local value = response_ch:receive()

		if value ~= "awaited_response" then
			return nil, "expected 'awaited_response', got " .. tostring(value)
		end

		return "ok"
	`

	proc := startResponseTestProcess(t, script)
	defer proc.Close()

	// Run until idle (blocked on receive)
	if err := runResponseTestUntilIdle(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	// Verify process is waiting
	if !proc.HasSubscriptions() {
		t.Error("expected active subscriptions")
	}

	var output process.StepOutput

	// Send response to unblock
	if err := sendResponseTestMessage(proc, "blocking.response", payload.Payloads{payload.NewString("awaited_response")}, &output); err != nil {
		t.Fatal(err)
	}

	// Should complete now
	if err := runResponseTestUntilDone(t, proc, 50); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	t.Log("TestResponseChannelBlockingReceive passed")
}

// TestGovClientPattern simulates the gov client pattern:
// 1. Client creates response channel
// 2. Client would send request (simulated by message delivery)
// 3. Service replies to response channel
// 4. Client receives reply
func TestGovClientPattern(t *testing.T) {
	script := `
		-- Simulate gov client pattern
		local inbox_ch = channel.new(10)
		local response_ch = channel.new(10)

		-- Subscribe to inbox for incoming requests and response channel for replies
		subscribe("@pid/inbox", inbox_ch)
		subscribe("client.response.uuid-12345", response_ch)

		-- Client blocks waiting for response (like send_and_wait)
		local value = response_ch:receive()

		-- Validate response structure
		if type(value) ~= "table" then
			return nil, "expected table response, got " .. type(value)
		end

		if value.request_id ~= "req-xyz" then
			return nil, "expected request_id 'req-xyz', got " .. tostring(value.request_id)
		end

		if not value.success then
			return nil, "expected success to be true"
		end

		if value.data ~= "processed result" then
			return nil, "expected data 'processed result', got " .. tostring(value.data)
		end

		return "ok"
	`

	proc := startResponseTestProcess(t, script)
	defer proc.Close()

	// Run until blocked on response channel receive
	if err := runResponseTestUntilIdle(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	var output process.StepOutput

	// Simulate service sending response back to client (Go map, transcoder converts to Lua)
	responsePayload := payload.New(map[string]any{
		"request_id": "req-xyz",
		"success":    true,
		"data":       "processed result",
	})
	if err := sendResponseTestMessage(proc, "client.response.uuid-12345", payload.Payloads{responsePayload}, &output); err != nil {
		t.Fatal(err)
	}

	// Client should receive and complete
	if err := runResponseTestUntilDone(t, proc, 50); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	t.Log("TestGovClientPattern passed")
}

// TestListenReceivesRawPayloads verifies that channels created via subscribe()
// (like process.listen()) receive raw Lua values, NOT Message objects.
// This is critical for the response channel pattern where clients expect
// to access fields directly (response.request_id) not via :payload():data().
func TestListenReceivesRawPayloads(t *testing.T) {
	script := `
		local response_ch = channel.new(10)
		local control = channel.new(10)

		-- Subscribe to response topic (like process.listen())
		subscribe("listen.response.topic", response_ch)
		subscribe("control", control)

		-- Wait for message
		control:receive()

		-- Receive from response channel
		local result = channel.select{response_ch:case_receive(), default=true}

		if result.default then
			return nil, "response channel should have a message"
		end

		local value = result.value

		-- Value should be a raw table, NOT a Message object
		if type(value) ~= "table" then
			return nil, "expected table, got " .. type(value)
		end

		-- Verify it's NOT a Message object (no :from() or :payload() methods)
		if type(value.from) == "function" then
			return nil, "value should NOT be a Message object (has :from method)"
		end
		if type(value.payload) == "function" then
			return nil, "value should NOT be a Message object (has :payload method)"
		end

		-- Should be able to access fields directly
		if value.request_id ~= "test-123" then
			return nil, "expected request_id 'test-123', got " .. tostring(value.request_id)
		end

		if value.data ~= "payload_value" then
			return nil, "expected data 'payload_value', got " .. tostring(value.data)
		end

		return "ok"
	`

	proc := startResponseTestProcess(t, script)
	defer proc.Close()

	if err := runResponseTestUntilIdle(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	var output process.StepOutput

	// Send table payload
	responsePayload := payload.New(map[string]any{
		"request_id": "test-123",
		"data":       "payload_value",
	})
	if err := sendResponseTestMessage(proc, "listen.response.topic", payload.Payloads{responsePayload}, &output); err != nil {
		t.Fatal(err)
	}

	if err := sendResponseTestMessage(proc, "control", payload.Payloads{payload.NewString("go")}, &output); err != nil {
		t.Fatal(err)
	}

	if err := runResponseTestUntilDone(t, proc, 50); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	t.Log("TestListenReceivesRawPayloads passed")
}

// TestListenStringPayload verifies string payloads are received as raw strings.
func TestListenStringPayload(t *testing.T) {
	script := `
		local response_ch = channel.new(10)
		local control = channel.new(10)

		subscribe("listen.string.topic", response_ch)
		subscribe("control", control)

		control:receive()

		local result = channel.select{response_ch:case_receive(), default=true}

		if result.default then
			return nil, "response channel should have a message"
		end

		local value = result.value

		-- String payload should be received as string, not Message
		if type(value) ~= "string" then
			return nil, "expected string, got " .. type(value)
		end

		if value ~= "hello_world" then
			return nil, "expected 'hello_world', got " .. tostring(value)
		end

		return "ok"
	`

	proc := startResponseTestProcess(t, script)
	defer proc.Close()

	if err := runResponseTestUntilIdle(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	var output process.StepOutput

	// Send string payload
	if err := sendResponseTestMessage(proc, "listen.string.topic", payload.Payloads{payload.NewString("hello_world")}, &output); err != nil {
		t.Fatal(err)
	}

	if err := sendResponseTestMessage(proc, "control", payload.Payloads{payload.NewString("go")}, &output); err != nil {
		t.Fatal(err)
	}

	if err := runResponseTestUntilDone(t, proc, 50); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	t.Log("TestListenStringPayload passed")
}
