package engine

import (
	"context"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
	luapayload "github.com/wippyai/runtime/runtime/lua/engine/payload"
	systempayload "github.com/wippyai/runtime/system/payload"
	lua "github.com/wippyai/go-lua"
)

// Inbox Tests
//
// These tests verify inbox message routing and queue behavior:
// 1. Messages to topics with listeners go to those listeners only
// 2. Messages to topics without listeners fall through to inbox
// 3. System topics (starting with @) do NOT fall back to inbox
// 4. Inbox acts as catch-all for unmatched user topics
// 5. Messages queued before inbox subscription are delivered when subscription is created
//
// Tests use the Lua error-return pattern (return nil, "error message")
// to validate results within the Lua script.

// Test helpers

func createInboxTestTranscoder() payload.Transcoder {
	transcoder := systempayload.NewTranscoder()
	luapayload.Register(transcoder)
	return transcoder
}

func startInboxProcess(t *testing.T, script string) *Process {
	t.Helper()

	proto, err := lua.CompileString(script, "test.lua")
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

func startInboxProcessWithTranscoder(t *testing.T, script string) *Process {
	t.Helper()

	proto, err := lua.CompileString(script, "test.lua")
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	proc := mustNewProcess(t, WithProto(proto))

	rootCtx := ctxapi.NewRootContext()
	ctx, _ := ctxapi.OpenFrameContext(rootCtx)
	ctx = payload.WithTranscoder(ctx, createInboxTestTranscoder())

	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}

	LoadModuleDef(proc.State(), ChannelModule)
	loadPubSubGlobals(proc.State())

	return proc
}

func inboxRunUntilIdle(t *testing.T, proc *Process) error {
	t.Helper()
	var output process.StepOutput
	const maxSteps = 30
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

func inboxRunUntilDone(t *testing.T, proc *Process) error {
	t.Helper()
	var output process.StepOutput
	const maxSteps = 50
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

func sendInboxMessage(proc *Process, topic string, payloads payload.Payloads, output *process.StepOutput) error {
	events := []process.Event{{
		Type: process.EventMessage,
		Data: &relay.Package{
			Messages: []*relay.Message{{Topic: topic, Payloads: payloads}},
		},
	}}
	output.Reset()
	return proc.Step(events, output)
}

// Fallback Behavior Tests

func TestInbox_FallbackBehavior(t *testing.T) {
	script := `
		local inbox_ch = channel.new(10)
		local specific_ch = channel.new(10)
		local control = channel.new(10)

		subscribe("@pid/inbox", inbox_ch)
		subscribe("specific_topic", specific_ch)
		subscribe("control", control)

		-- Block until control message
		control:receive()

		-- Count messages by draining
		local specific_count = 0
		while true do
			local result = channel.select{specific_ch:case_receive(), default=true}
			if result.default then break end
			specific_count = specific_count + 1
		end

		local inbox_count = 0
		while true do
			local result = channel.select{inbox_ch:case_receive(), default=true}
			if result.default then break end
			inbox_count = inbox_count + 1
		end

		-- Validate inside Lua and return error if wrong
		if specific_count ~= 2 then
			return nil, "specific_topic should have 2 messages, got " .. specific_count
		end
		if inbox_count ~= 1 then
			return nil, "inbox should have 1 message, got " .. inbox_count
		end
		return "ok"
	`

	proc := startInboxProcess(t, script)
	defer proc.Close()

	if err := inboxRunUntilIdle(t, proc); err != nil {
		t.Fatal(err)
	}

	if !proc.HasSubscriptions() {
		t.Error("expected subscriptions to be active")
	}

	var output process.StepOutput

	// Message to specific_topic (should go to specific listener, NOT inbox)
	if err := sendInboxMessage(proc, "specific_topic", payload.Payloads{payload.NewPayload(lua.LString("msg1"), payload.Lua)}, &output); err != nil {
		t.Fatalf("send to specific_topic failed: %v", err)
	}

	// Message to unmatched topic (should fall through to inbox)
	if err := sendInboxMessage(proc, "random_topic", payload.Payloads{payload.NewPayload(lua.LString("msg2"), payload.Lua)}, &output); err != nil {
		t.Fatalf("send to random_topic failed: %v", err)
	}

	// Another message to specific_topic
	if err := sendInboxMessage(proc, "specific_topic", payload.Payloads{payload.NewPayload(lua.LString("msg3"), payload.Lua)}, &output); err != nil {
		t.Fatalf("send second to specific_topic failed: %v", err)
	}

	// Signal to start counting
	if err := sendInboxMessage(proc, "control", payload.Payloads{payload.NewPayload(lua.LString("go"), payload.Lua)}, &output); err != nil {
		t.Fatalf("send control failed: %v", err)
	}

	if err := inboxRunUntilDone(t, proc); err != nil {
		t.Fatalf("Process failed: %v", err)
	}
}

func TestInbox_DoesNotReceiveMatchedTopics(t *testing.T) {
	script := `
		local inbox_ch = channel.new(10)
		local specific_ch = channel.new(10)
		local control = channel.new(10)

		subscribe("@pid/inbox", inbox_ch)
		subscribe("my_topic", specific_ch)
		subscribe("control", control)

		control:receive()

		-- Drain and count
		local function count_messages(ch)
			local count = 0
			while true do
				local result = channel.select{ch:case_receive(), default=true}
				if result.default then break end
				count = count + 1
			end
			return count
		end

		local specific_count = count_messages(specific_ch)
		local inbox_count = count_messages(inbox_ch)

		-- Validate inside Lua
		if specific_count ~= 1 then
			return nil, "my_topic should have 1 message, got " .. specific_count
		end
		if inbox_count ~= 0 then
			return nil, "inbox should have 0 messages (topic matched), got " .. inbox_count
		end
		return "ok"
	`

	proc := startInboxProcess(t, script)
	defer proc.Close()

	if err := inboxRunUntilIdle(t, proc); err != nil {
		t.Fatal(err)
	}

	// Send ONLY to the topic that has a listener
	var output process.StepOutput
	if err := sendInboxMessage(proc, "my_topic", payload.Payloads{payload.NewPayload(lua.LString("test"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}

	// Signal to start counting
	if err := sendInboxMessage(proc, "control", payload.Payloads{payload.NewPayload(lua.LString("go"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}

	if err := inboxRunUntilDone(t, proc); err != nil {
		t.Fatalf("Process failed: %v", err)
	}
}

func TestInbox_ReceivesUnmatchedTopics(t *testing.T) {
	script := `
		local inbox_ch = channel.new(10)
		local control = channel.new(10)

		-- Subscribe ONLY to inbox and control
		subscribe("@pid/inbox", inbox_ch)
		subscribe("control", control)

		control:receive()

		local function count_messages(ch)
			local count = 0
			while true do
				local result = channel.select{ch:case_receive(), default=true}
				if result.default then break end
				count = count + 1
			end
			return count
		end

		local inbox_count = count_messages(inbox_ch)

		-- All 3 messages should fall through to inbox
		if inbox_count ~= 3 then
			return nil, "inbox should have 3 messages (all unmatched), got " .. inbox_count
		end
		return "ok"
	`

	proc := startInboxProcess(t, script)
	defer proc.Close()

	if err := inboxRunUntilIdle(t, proc); err != nil {
		t.Fatal(err)
	}

	// Send to random topics (none have listeners)
	var output process.StepOutput
	if err := sendInboxMessage(proc, "topic_a", payload.Payloads{payload.NewPayload(lua.LString("a"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}
	if err := sendInboxMessage(proc, "topic_b", payload.Payloads{payload.NewPayload(lua.LString("b"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}
	if err := sendInboxMessage(proc, "topic_c", payload.Payloads{payload.NewPayload(lua.LString("c"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}

	// Signal to count
	if err := sendInboxMessage(proc, "control", payload.Payloads{payload.NewPayload(lua.LString("go"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}

	if err := inboxRunUntilDone(t, proc); err != nil {
		t.Fatalf("Process failed: %v", err)
	}
}

func TestInbox_SystemTopicsDoNotFallback(t *testing.T) {
	script := `
		local inbox_ch = channel.new(10)
		local control = channel.new(10)

		subscribe("@pid/inbox", inbox_ch)
		subscribe("control", control)

		control:receive()

		local function count_messages(ch)
			local count = 0
			while true do
				local result = channel.select{ch:case_receive(), default=true}
				if result.default then break end
				count = count + 1
			end
			return count
		end

		local inbox_count = count_messages(inbox_ch)

		-- System topic (@...) should NOT fall back to inbox
		if inbox_count ~= 0 then
			return nil, "inbox should NOT receive @ topics, got " .. inbox_count .. " messages"
		end
		return "ok"
	`

	proc := startInboxProcess(t, script)
	defer proc.Close()

	if err := inboxRunUntilIdle(t, proc); err != nil {
		t.Fatal(err)
	}

	// Send to a system topic that doesn't have a listener
	var output process.StepOutput
	if err := sendInboxMessage(proc, "@system/unknown", payload.Payloads{payload.NewPayload(lua.LString("test"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}

	// Signal to count
	if err := sendInboxMessage(proc, "control", payload.Payloads{payload.NewPayload(lua.LString("go"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}

	if err := inboxRunUntilDone(t, proc); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Also verify message remained in queue (undelivered)
	if len(proc.messageQueue) != 1 {
		t.Errorf("@ topic message should remain in queue, queue len: %d", len(proc.messageQueue))
	}
}

func TestInbox_MultipleListenersRoutingPriority(t *testing.T) {
	script := `
		local inbox_ch = channel.new(10)
		local topic_a_ch = channel.new(10)
		local topic_b_ch = channel.new(10)
		local control = channel.new(10)

		subscribe("@pid/inbox", inbox_ch)
		subscribe("topic_a", topic_a_ch)
		subscribe("topic_b", topic_b_ch)
		subscribe("control", control)

		control:receive()

		local function count_messages(ch)
			local count = 0
			while true do
				local result = channel.select{ch:case_receive(), default=true}
				if result.default then break end
				count = count + 1
			end
			return count
		end

		local topic_a_count = count_messages(topic_a_ch)
		local topic_b_count = count_messages(topic_b_ch)
		local inbox_count = count_messages(inbox_ch)

		-- Validate
		if topic_a_count ~= 1 then
			return nil, "topic_a should have 1 message, got " .. topic_a_count
		end
		if topic_b_count ~= 1 then
			return nil, "topic_b should have 1 message, got " .. topic_b_count
		end
		if inbox_count ~= 1 then
			return nil, "inbox should have 1 message (only unmatched), got " .. inbox_count
		end
		return "ok"
	`

	proc := startInboxProcess(t, script)
	defer proc.Close()

	if err := inboxRunUntilIdle(t, proc); err != nil {
		t.Fatal(err)
	}

	var output process.StepOutput
	// Send to topic_a
	if err := sendInboxMessage(proc, "topic_a", payload.Payloads{payload.NewPayload(lua.LString("for_a"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}
	// Send to topic_b
	if err := sendInboxMessage(proc, "topic_b", payload.Payloads{payload.NewPayload(lua.LString("for_b"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}
	// Send to unmatched topic
	if err := sendInboxMessage(proc, "topic_c", payload.Payloads{payload.NewPayload(lua.LString("for_inbox"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}

	// Signal to count
	if err := sendInboxMessage(proc, "control", payload.Payloads{payload.NewPayload(lua.LString("go"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}

	if err := inboxRunUntilDone(t, proc); err != nil {
		t.Fatalf("Process failed: %v", err)
	}
}

// Queue Flush Behavior Tests

func TestInbox_QueuedBeforeSubscription(t *testing.T) {
	script := `
		local control = channel.new(1)
		subscribe("control", control)

		-- Wait for control signal (inbox doesn't exist yet)
		control:receive()

		-- NOW create inbox subscription
		local inbox_ch = channel.new(10)
		subscribe("@pid/inbox", inbox_ch)

		-- Try to receive from inbox (message should already be there from queue flush)
		local result = channel.select{inbox_ch:case_receive(), default=true}
		if result.default then
			return nil, "inbox should have received queued message, but was empty"
		end

		-- Verify message content
		local msg = result.value
		if msg ~= "early_message" then
			return nil, "expected 'early_message', got: " .. tostring(msg)
		end

		return "ok"
	`

	proc := startInboxProcess(t, script)
	defer proc.Close()

	// Run until idle (blocked on control:receive)
	if err := inboxRunUntilIdle(t, proc); err != nil {
		t.Fatal(err)
	}

	// Verify inbox subscription does NOT exist yet
	if proc.subs != nil {
		_, inboxExists := proc.subs.get(topology.TopicInbox)
		if inboxExists {
			t.Fatal("inbox subscription should not exist yet")
		}
	}

	// Send message to "my_topic" - no inbox yet, should be queued
	var output process.StepOutput
	if err := sendInboxMessage(proc, "my_topic", payload.Payloads{payload.NewPayload(lua.LString("early_message"), payload.Lua)}, &output); err != nil {
		t.Fatalf("send message failed: %v", err)
	}

	// Verify message is in queue (not delivered because no inbox)
	if len(proc.messageQueue) != 1 {
		t.Fatalf("message should be queued, queue len: %d", len(proc.messageQueue))
	}

	// Signal control to continue (this will create inbox subscription)
	if err := sendInboxMessage(proc, "control", payload.Payloads{payload.NewPayload(lua.LString("go"), payload.Lua)}, &output); err != nil {
		t.Fatalf("send control failed: %v", err)
	}

	// Run until done
	if err := inboxRunUntilDone(t, proc); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Verify queue was flushed
	if len(proc.messageQueue) != 0 {
		t.Errorf("message queue should be empty after inbox subscription, len: %d", len(proc.messageQueue))
	}
}

func TestInbox_MultipleQueuedBeforeSubscription(t *testing.T) {
	script := `
		local control = channel.new(1)
		subscribe("control", control)

		-- Wait for control signal (inbox doesn't exist yet)
		control:receive()

		-- NOW create inbox subscription
		local inbox_ch = channel.new(10)
		subscribe("@pid/inbox", inbox_ch)

		-- Receive all messages and verify order
		local received = {}
		for i = 1, 3 do
			local result = channel.select{inbox_ch:case_receive(), default=true}
			if result.default then
				return nil, "expected 3 messages, got only " .. (#received)
			end
			table.insert(received, result.value)
		end

		-- Verify order
		if received[1] ~= "msg1" then return nil, "msg1 wrong: " .. tostring(received[1]) end
		if received[2] ~= "msg2" then return nil, "msg2 wrong: " .. tostring(received[2]) end
		if received[3] ~= "msg3" then return nil, "msg3 wrong: " .. tostring(received[3]) end

		return "ok"
	`

	proc := startInboxProcess(t, script)
	defer proc.Close()

	if err := inboxRunUntilIdle(t, proc); err != nil {
		t.Fatal(err)
	}

	var output process.StepOutput

	// Send multiple messages before inbox subscription
	if err := sendInboxMessage(proc, "topic_a", payload.Payloads{payload.NewPayload(lua.LString("msg1"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}
	if err := sendInboxMessage(proc, "topic_b", payload.Payloads{payload.NewPayload(lua.LString("msg2"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}
	if err := sendInboxMessage(proc, "topic_c", payload.Payloads{payload.NewPayload(lua.LString("msg3"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}

	// Verify all queued
	if len(proc.messageQueue) != 3 {
		t.Fatalf("expected 3 messages in queue, got %d", len(proc.messageQueue))
	}

	// Signal to create inbox subscription
	if err := sendInboxMessage(proc, "control", payload.Payloads{payload.NewPayload(lua.LString("go"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}

	if err := inboxRunUntilDone(t, proc); err != nil {
		t.Fatalf("Process failed: %v", err)
	}
}

func TestInbox_MixedDeliveryBeforeAndAfter(t *testing.T) {
	script := `
		local control = channel.new(1)
		subscribe("control", control)

		-- Phase 1: Wait before creating inbox
		control:receive()

		-- Create inbox subscription
		local inbox_ch = channel.new(10)
		subscribe("@pid/inbox", inbox_ch)

		-- Phase 2: Wait for more messages after inbox exists
		control:receive()

		-- Count all messages in inbox
		local count = 0
		while true do
			local result = channel.select{inbox_ch:case_receive(), default=true}
			if result.default then break end
			count = count + 1
		end

		-- Should have 2 messages: 1 from before, 1 from after
		if count ~= 2 then
			return nil, "expected 2 messages, got " .. count
		end

		return "ok"
	`

	proc := startInboxProcess(t, script)
	defer proc.Close()

	if err := inboxRunUntilIdle(t, proc); err != nil {
		t.Fatal(err)
	}

	var output process.StepOutput

	// Send message BEFORE inbox subscription
	if err := sendInboxMessage(proc, "early_topic", payload.Payloads{payload.NewPayload(lua.LString("early"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}

	// Verify queued
	if len(proc.messageQueue) != 1 {
		t.Fatalf("expected 1 message in queue before inbox, got %d", len(proc.messageQueue))
	}

	// Signal phase 1 complete - this creates inbox subscription
	if err := sendInboxMessage(proc, "control", payload.Payloads{payload.NewPayload(lua.LString("phase1"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}

	// Run until idle again (waiting on second control:receive)
	if err := inboxRunUntilIdle(t, proc); err != nil {
		t.Fatal(err)
	}

	// Verify queue was flushed by inbox subscription
	if len(proc.messageQueue) != 0 {
		t.Errorf("queue should be empty after inbox subscription, got %d", len(proc.messageQueue))
	}

	// Send message AFTER inbox subscription exists
	if err := sendInboxMessage(proc, "late_topic", payload.Payloads{payload.NewPayload(lua.LString("late"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}

	// Signal phase 2 complete
	if err := sendInboxMessage(proc, "control", payload.Payloads{payload.NewPayload(lua.LString("phase2"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}

	if err := inboxRunUntilDone(t, proc); err != nil {
		t.Fatalf("Process failed: %v", err)
	}
}

func TestInbox_SystemTopicNotQueued(t *testing.T) {
	script := `
		local control = channel.new(1)
		subscribe("control", control)

		control:receive()

		local inbox_ch = channel.new(10)
		subscribe("@pid/inbox", inbox_ch)

		-- Inbox should be empty - @ topic should NOT fallback
		local result = channel.select{inbox_ch:case_receive(), default=true}
		if not result.default then
			return nil, "@ topic should NOT be delivered to inbox"
		end

		return "ok"
	`

	proc := startInboxProcess(t, script)
	defer proc.Close()

	if err := inboxRunUntilIdle(t, proc); err != nil {
		t.Fatal(err)
	}

	var output process.StepOutput

	// Send @ topic message before inbox
	if err := sendInboxMessage(proc, "@system/test", payload.Payloads{payload.NewPayload(lua.LString("system"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}

	// Verify queued (won't be delivered to inbox because @ prefix)
	if len(proc.messageQueue) != 1 {
		t.Fatalf("@ topic should be queued, got %d", len(proc.messageQueue))
	}

	// Signal to create inbox
	if err := sendInboxMessage(proc, "control", payload.Payloads{payload.NewPayload(lua.LString("go"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}

	if err := inboxRunUntilDone(t, proc); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// @ topic should STILL be in queue (not delivered to inbox)
	if len(proc.messageQueue) != 1 {
		t.Errorf("@ topic should remain in queue, got %d", len(proc.messageQueue))
	}
}

func TestInbox_QueueFlushOnEachSubscription(t *testing.T) {
	script := `
		local control = channel.new(1)
		subscribe("control", control)

		control:receive()

		-- Create subscription for "my_topic" - should flush queued message
		local topic_ch = channel.new(10)
		subscribe("my_topic", topic_ch)

		-- Message should have been delivered
		local result = channel.select{topic_ch:case_receive(), default=true}
		if result.default then
			return nil, "queued message should be delivered when subscription created"
		end

		if result.value ~= "early_value" then
			return nil, "wrong message: " .. tostring(result.value)
		end

		return "ok"
	`

	proc := startInboxProcess(t, script)
	defer proc.Close()

	if err := inboxRunUntilIdle(t, proc); err != nil {
		t.Fatal(err)
	}

	var output process.StepOutput

	// Send to "my_topic" before subscription exists
	if err := sendInboxMessage(proc, "my_topic", payload.Payloads{payload.NewPayload(lua.LString("early_value"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}

	// Verify queued
	if len(proc.messageQueue) != 1 {
		t.Fatalf("message should be queued, got %d", len(proc.messageQueue))
	}

	// Signal to create "my_topic" subscription
	if err := sendInboxMessage(proc, "control", payload.Payloads{payload.NewPayload(lua.LString("go"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}

	if err := inboxRunUntilDone(t, proc); err != nil {
		t.Fatalf("Process failed: %v", err)
	}
}

func TestInbox_DirectQueueInjection(t *testing.T) {
	script := `
		-- First thing: create inbox subscription
		local inbox_ch = channel.new(10)
		subscribe("@pid/inbox", inbox_ch)

		-- Message was injected before we even started - should be there now
		local result = channel.select{inbox_ch:case_receive(), default=true}
		if result.default then
			return nil, "pre-injected message should be delivered on subscription"
		end

		if result.value ~= "pre_injected" then
			return nil, "wrong value: " .. tostring(result.value)
		end

		return "ok"
	`

	proc := startInboxProcess(t, script)
	defer proc.Close()

	// Directly inject a message into the queue BEFORE any step execution
	proc.messageQueue = append(proc.messageQueue, queuedMessage{
		Topic:    "injected_topic",
		Payloads: payload.Payloads{payload.NewPayload(lua.LString("pre_injected"), payload.Lua)},
	})

	// Now run - inbox subscription should flush the pre-injected message
	if err := inboxRunUntilDone(t, proc); err != nil {
		t.Fatalf("Process failed: %v", err)
	}
}

// Response Channel Pattern Tests
//
// These tests verify the response channel pattern used by gov client/service:
// 1. Client creates a response channel using process.listen()
// 2. Client sends request with respond_to field
// 3. Service processes request and sends reply to respond_to channel
// 4. Client receives reply on the response channel

func TestResponseChannelBasic(t *testing.T) {
	script := `
		local response_ch = channel.new(10)
		local control = channel.new(10)

		subscribe("test.response.12345", response_ch)
		subscribe("control", control)

		control:receive()

		local result = channel.select{response_ch:case_receive(), default=true}

		if result.default then
			return nil, "response channel should have a message"
		end

		if result.value ~= "response_data" then
			return nil, "expected 'response_data', got " .. tostring(result.value)
		end

		return "ok"
	`

	proc := startInboxProcessWithTranscoder(t, script)
	defer proc.Close()

	if err := inboxRunUntilIdle(t, proc); err != nil {
		t.Fatal(err)
	}

	var output process.StepOutput

	if err := sendInboxMessage(proc, "test.response.12345", payload.Payloads{payload.NewPayload(lua.LString("response_data"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}

	if err := sendInboxMessage(proc, "control", payload.Payloads{payload.NewPayload(lua.LString("go"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}

	if err := inboxRunUntilDone(t, proc); err != nil {
		t.Fatalf("Process failed: %v", err)
	}
}

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

	proc := startInboxProcessWithTranscoder(t, script)
	defer proc.Close()

	if err := inboxRunUntilIdle(t, proc); err != nil {
		t.Fatal(err)
	}

	var output process.StepOutput

	responsePayload := payload.New(map[string]any{
		"request_id": "req-001",
		"success":    true,
		"result":     "processed data",
	})
	if err := sendInboxMessage(proc, "test.response.table", payload.Payloads{responsePayload}, &output); err != nil {
		t.Fatal(err)
	}

	if err := sendInboxMessage(proc, "control", payload.Payloads{payload.NewPayload(lua.LString("go"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}

	if err := inboxRunUntilDone(t, proc); err != nil {
		t.Fatalf("Process failed: %v", err)
	}
}

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

	proc := startInboxProcessWithTranscoder(t, script)
	defer proc.Close()

	if err := inboxRunUntilIdle(t, proc); err != nil {
		t.Fatal(err)
	}

	var output process.StepOutput

	if err := sendInboxMessage(proc, "response.ch1", payload.Payloads{payload.NewPayload(lua.LString("r1"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}
	if err := sendInboxMessage(proc, "response.ch2", payload.Payloads{payload.NewPayload(lua.LString("r2a"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}
	if err := sendInboxMessage(proc, "response.ch2", payload.Payloads{payload.NewPayload(lua.LString("r2b"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}

	if err := sendInboxMessage(proc, "control", payload.Payloads{payload.NewPayload(lua.LString("go"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}

	if err := inboxRunUntilDone(t, proc); err != nil {
		t.Fatalf("Process failed: %v", err)
	}
}

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

	proc := startInboxProcessWithTranscoder(t, script)
	defer proc.Close()

	if err := inboxRunUntilIdle(t, proc); err != nil {
		t.Fatal(err)
	}

	var output process.StepOutput

	if err := sendInboxMessage(proc, "my.response.topic", payload.Payloads{payload.NewPayload(lua.LString("response"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}

	if err := sendInboxMessage(proc, "control", payload.Payloads{payload.NewPayload(lua.LString("go"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}

	if err := inboxRunUntilDone(t, proc); err != nil {
		t.Fatalf("Process failed: %v", err)
	}
}

func TestResponseChannelBlockingReceive(t *testing.T) {
	script := `
		local response_ch = channel.new(10)
		subscribe("blocking.response", response_ch)

		local value = response_ch:receive()

		if value ~= "awaited_response" then
			return nil, "expected 'awaited_response', got " .. tostring(value)
		end

		return "ok"
	`

	proc := startInboxProcessWithTranscoder(t, script)
	defer proc.Close()

	if err := inboxRunUntilIdle(t, proc); err != nil {
		t.Fatal(err)
	}

	if !proc.HasSubscriptions() {
		t.Error("expected active subscriptions")
	}

	var output process.StepOutput

	if err := sendInboxMessage(proc, "blocking.response", payload.Payloads{payload.NewPayload(lua.LString("awaited_response"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}

	if err := inboxRunUntilDone(t, proc); err != nil {
		t.Fatalf("Process failed: %v", err)
	}
}

func TestGovClientPattern(t *testing.T) {
	script := `
		local inbox_ch = channel.new(10)
		local response_ch = channel.new(10)

		subscribe("@pid/inbox", inbox_ch)
		subscribe("client.response.uuid-12345", response_ch)

		local value = response_ch:receive()

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

	proc := startInboxProcessWithTranscoder(t, script)
	defer proc.Close()

	if err := inboxRunUntilIdle(t, proc); err != nil {
		t.Fatal(err)
	}

	var output process.StepOutput

	responsePayload := payload.New(map[string]any{
		"request_id": "req-xyz",
		"success":    true,
		"data":       "processed result",
	})
	if err := sendInboxMessage(proc, "client.response.uuid-12345", payload.Payloads{responsePayload}, &output); err != nil {
		t.Fatal(err)
	}

	if err := inboxRunUntilDone(t, proc); err != nil {
		t.Fatalf("Process failed: %v", err)
	}
}

func TestListenReceivesRawPayloads(t *testing.T) {
	script := `
		local response_ch = channel.new(10)
		local control = channel.new(10)

		subscribe("listen.response.topic", response_ch)
		subscribe("control", control)

		control:receive()

		local result = channel.select{response_ch:case_receive(), default=true}

		if result.default then
			return nil, "response channel should have a message"
		end

		local value = result.value

		if type(value) ~= "table" then
			return nil, "expected table, got " .. type(value)
		end

		if type(value.from) == "function" then
			return nil, "value should NOT be a Message object (has :from method)"
		end
		if type(value.payload) == "function" then
			return nil, "value should NOT be a Message object (has :payload method)"
		end

		if value.request_id ~= "test-123" then
			return nil, "expected request_id 'test-123', got " .. tostring(value.request_id)
		end

		if value.data ~= "payload_value" then
			return nil, "expected data 'payload_value', got " .. tostring(value.data)
		end

		return "ok"
	`

	proc := startInboxProcessWithTranscoder(t, script)
	defer proc.Close()

	if err := inboxRunUntilIdle(t, proc); err != nil {
		t.Fatal(err)
	}

	var output process.StepOutput

	responsePayload := payload.New(map[string]any{
		"request_id": "test-123",
		"data":       "payload_value",
	})
	if err := sendInboxMessage(proc, "listen.response.topic", payload.Payloads{responsePayload}, &output); err != nil {
		t.Fatal(err)
	}

	if err := sendInboxMessage(proc, "control", payload.Payloads{payload.NewPayload(lua.LString("go"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}

	if err := inboxRunUntilDone(t, proc); err != nil {
		t.Fatalf("Process failed: %v", err)
	}
}

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

		if type(value) ~= "string" then
			return nil, "expected string, got " .. type(value)
		end

		if value ~= "hello_world" then
			return nil, "expected 'hello_world', got " .. tostring(value)
		end

		return "ok"
	`

	proc := startInboxProcessWithTranscoder(t, script)
	defer proc.Close()

	if err := inboxRunUntilIdle(t, proc); err != nil {
		t.Fatal(err)
	}

	var output process.StepOutput

	if err := sendInboxMessage(proc, "listen.string.topic", payload.Payloads{payload.NewPayload(lua.LString("hello_world"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}

	if err := sendInboxMessage(proc, "control", payload.Payloads{payload.NewPayload(lua.LString("go"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}

	if err := inboxRunUntilDone(t, proc); err != nil {
		t.Fatalf("Process failed: %v", err)
	}
}

// Message Ordering Tests
//
// These tests verify that messages sent BEFORE subscription are not lost
// and are delivered when subscription is created.

func TestMessagesBeforeSubscription(t *testing.T) {
	script := `
		local x = 1 + 1

		local ch = channel.new(10)
		subscribe("my_topic", ch)

		local result = channel.select{ch:case_receive(), default=true}

		if result.default then
			return nil, "message sent before subscription was LOST"
		end

		if result.value ~= "early_message" then
			return nil, "expected 'early_message', got " .. tostring(result.value)
		end

		return "ok"
	`

	proc := startInboxProcessWithTranscoder(t, script)
	defer proc.Close()

	var output process.StepOutput

	proc.messageQueue = append(proc.messageQueue, queuedMessage{
		Topic:    "my_topic",
		Payloads: payload.Payloads{payload.NewPayload(lua.LString("early_message"), payload.Lua)},
	})

	for i := 0; i < 50; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("step %d failed: %v", i, err)
		}
		if output.Status() == process.StepDone {
			break
		}
	}

	if output.Status() != process.StepDone {
		t.Fatal("process did not complete")
	}
}

func TestMessagesBeforeInboxSubscription(t *testing.T) {
	script := `
		local x = 1 + 1

		local inbox_ch = channel.new(10)
		subscribe("@pid/inbox", inbox_ch)

		local result = channel.select{inbox_ch:case_receive(), default=true}

		if result.default then
			return nil, "message sent before inbox subscription was LOST"
		end

		if result.value ~= "early_inbox_message" then
			return nil, "expected 'early_inbox_message', got " .. tostring(result.value)
		end

		return "ok"
	`

	proc := startInboxProcessWithTranscoder(t, script)
	defer proc.Close()

	var output process.StepOutput

	proc.messageQueue = append(proc.messageQueue, queuedMessage{
		Topic:    "random_topic",
		Payloads: payload.Payloads{payload.NewPayload(lua.LString("early_inbox_message"), payload.Lua)},
	})

	for i := 0; i < 50; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("step %d failed: %v", i, err)
		}
		if output.Status() == process.StepDone {
			break
		}
	}

	if output.Status() != process.StepDone {
		t.Fatal("process did not complete")
	}
}

func TestMessageViaStepBeforeSubscription(t *testing.T) {
	script := `
		coroutine.yield()

		local ch = channel.new(10)
		subscribe("my_topic", ch)

		local result = channel.select{ch:case_receive(), default=true}

		if result.default then
			return nil, "message was LOST"
		end

		if result.value ~= "test" then
			return nil, "expected 'test', got " .. tostring(result.value)
		end

		return "ok"
	`

	proc := startInboxProcessWithTranscoder(t, script)
	defer proc.Close()

	var output process.StepOutput

	output.Reset()
	if err := proc.Step(nil, &output); err != nil {
		t.Fatal(err)
	}

	if err := sendInboxMessage(proc, "my_topic", payload.Payloads{payload.NewPayload(lua.LString("test"), payload.Lua)}, &output); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 50; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("step %d failed: %v", i, err)
		}
		if output.Status() == process.StepDone {
			break
		}
	}

	if output.Status() != process.StepDone {
		t.Fatal("process did not complete")
	}
}

func TestMessageQueueState(t *testing.T) {
	script := `
		local ch = channel.new(10)
		subscribe("test_topic", ch)

		local value = ch:receive()
		return value
	`

	proc := startInboxProcessWithTranscoder(t, script)
	defer proc.Close()

	var output process.StepOutput

	proc.messageQueue = append(proc.messageQueue, queuedMessage{
		Topic:    "test_topic",
		Payloads: payload.Payloads{payload.NewPayload(lua.LString("msg1"), payload.Lua)},
	})

	for i := 0; i < 30; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatal(err)
		}
		if output.Status() == process.StepIdle || output.Status() == process.StepDone {
			break
		}
	}

	if len(proc.messageQueue) != 0 {
		t.Errorf("expected queue to be empty after subscription, got %d", len(proc.messageQueue))
	}
}

func TestResponseChannelEarlyMessage(t *testing.T) {
	script := `
		local response_ch = channel.new(10)
		subscribe("response.123", response_ch)

		local x = 0
		for i = 1, 100 do
			x = x + i
		end

		local value = response_ch:receive()

		if type(value) ~= "table" then
			return nil, "expected table response, got " .. type(value)
		end

		if value.result ~= "success" then
			return nil, "expected result 'success', got " .. tostring(value.result)
		end

		return "ok"
	`

	proc := startInboxProcessWithTranscoder(t, script)
	defer proc.Close()

	var output process.StepOutput

	for i := 0; i < 100; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatal(err)
		}
		if output.Status() == process.StepIdle {
			break
		}
	}

	if !proc.HasSubscriptions() {
		t.Fatal("expected subscriptions")
	}

	responsePayload := payload.New(map[string]any{
		"result": "success",
	})
	if err := sendInboxMessage(proc, "response.123", payload.Payloads{responsePayload}, &output); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 50; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("step failed: %v", err)
		}
		if output.Status() == process.StepDone {
			break
		}
	}

	if output.Status() != process.StepDone {
		t.Fatal("process did not complete")
	}
}
