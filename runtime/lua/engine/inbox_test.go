package engine

import (
	"context"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
	lua "github.com/yuin/gopher-lua"
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

func startInboxProcess(t *testing.T, script string) *Process {
	t.Helper()

	proto, err := lua.CompileString(script, "test.lua")
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	proc := NewProcess(WithProto(proto))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}

	ChannelModule.Load(proc.State())
	loadPubSubGlobals(proc.State())

	return proc
}

func inboxRunUntilIdle(t *testing.T, proc *Process, maxSteps int) error {
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

func inboxRunUntilDone(t *testing.T, proc *Process, maxSteps int) error {
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

	if err := inboxRunUntilIdle(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	if !proc.HasSubscriptions() {
		t.Error("expected subscriptions to be active")
	}

	var output process.StepOutput

	// Message to specific_topic (should go to specific listener, NOT inbox)
	if err := sendInboxMessage(proc, "specific_topic", payload.Payloads{payload.NewString("msg1")}, &output); err != nil {
		t.Fatalf("send to specific_topic failed: %v", err)
	}

	// Message to unmatched topic (should fall through to inbox)
	if err := sendInboxMessage(proc, "random_topic", payload.Payloads{payload.NewString("msg2")}, &output); err != nil {
		t.Fatalf("send to random_topic failed: %v", err)
	}

	// Another message to specific_topic
	if err := sendInboxMessage(proc, "specific_topic", payload.Payloads{payload.NewString("msg3")}, &output); err != nil {
		t.Fatalf("send second to specific_topic failed: %v", err)
	}

	// Signal to start counting
	if err := sendInboxMessage(proc, "control", payload.Payloads{payload.NewString("go")}, &output); err != nil {
		t.Fatalf("send control failed: %v", err)
	}

	if err := inboxRunUntilDone(t, proc, 50); err != nil {
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

	if err := inboxRunUntilIdle(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	// Send ONLY to the topic that has a listener
	var output process.StepOutput
	if err := sendInboxMessage(proc, "my_topic", payload.Payloads{payload.NewString("test")}, &output); err != nil {
		t.Fatal(err)
	}

	// Signal to start counting
	if err := sendInboxMessage(proc, "control", payload.Payloads{payload.NewString("go")}, &output); err != nil {
		t.Fatal(err)
	}

	if err := inboxRunUntilDone(t, proc, 50); err != nil {
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

	if err := inboxRunUntilIdle(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	// Send to random topics (none have listeners)
	var output process.StepOutput
	if err := sendInboxMessage(proc, "topic_a", payload.Payloads{payload.NewString("a")}, &output); err != nil {
		t.Fatal(err)
	}
	if err := sendInboxMessage(proc, "topic_b", payload.Payloads{payload.NewString("b")}, &output); err != nil {
		t.Fatal(err)
	}
	if err := sendInboxMessage(proc, "topic_c", payload.Payloads{payload.NewString("c")}, &output); err != nil {
		t.Fatal(err)
	}

	// Signal to count
	if err := sendInboxMessage(proc, "control", payload.Payloads{payload.NewString("go")}, &output); err != nil {
		t.Fatal(err)
	}

	if err := inboxRunUntilDone(t, proc, 50); err != nil {
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

	if err := inboxRunUntilIdle(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	// Send to a system topic that doesn't have a listener
	var output process.StepOutput
	if err := sendInboxMessage(proc, "@system/unknown", payload.Payloads{payload.NewString("test")}, &output); err != nil {
		t.Fatal(err)
	}

	// Signal to count
	if err := sendInboxMessage(proc, "control", payload.Payloads{payload.NewString("go")}, &output); err != nil {
		t.Fatal(err)
	}

	if err := inboxRunUntilDone(t, proc, 50); err != nil {
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

	if err := inboxRunUntilIdle(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	var output process.StepOutput
	// Send to topic_a
	if err := sendInboxMessage(proc, "topic_a", payload.Payloads{payload.NewString("for_a")}, &output); err != nil {
		t.Fatal(err)
	}
	// Send to topic_b
	if err := sendInboxMessage(proc, "topic_b", payload.Payloads{payload.NewString("for_b")}, &output); err != nil {
		t.Fatal(err)
	}
	// Send to unmatched topic
	if err := sendInboxMessage(proc, "topic_c", payload.Payloads{payload.NewString("for_inbox")}, &output); err != nil {
		t.Fatal(err)
	}

	// Signal to count
	if err := sendInboxMessage(proc, "control", payload.Payloads{payload.NewString("go")}, &output); err != nil {
		t.Fatal(err)
	}

	if err := inboxRunUntilDone(t, proc, 50); err != nil {
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
	if err := inboxRunUntilIdle(t, proc, 30); err != nil {
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
	if err := sendInboxMessage(proc, "my_topic", payload.Payloads{payload.NewString("early_message")}, &output); err != nil {
		t.Fatalf("send message failed: %v", err)
	}

	// Verify message is in queue (not delivered because no inbox)
	if len(proc.messageQueue) != 1 {
		t.Fatalf("message should be queued, queue len: %d", len(proc.messageQueue))
	}

	// Signal control to continue (this will create inbox subscription)
	if err := sendInboxMessage(proc, "control", payload.Payloads{payload.NewString("go")}, &output); err != nil {
		t.Fatalf("send control failed: %v", err)
	}

	// Run until done
	if err := inboxRunUntilDone(t, proc, 50); err != nil {
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

	if err := inboxRunUntilIdle(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	var output process.StepOutput

	// Send multiple messages before inbox subscription
	if err := sendInboxMessage(proc, "topic_a", payload.Payloads{payload.NewString("msg1")}, &output); err != nil {
		t.Fatal(err)
	}
	if err := sendInboxMessage(proc, "topic_b", payload.Payloads{payload.NewString("msg2")}, &output); err != nil {
		t.Fatal(err)
	}
	if err := sendInboxMessage(proc, "topic_c", payload.Payloads{payload.NewString("msg3")}, &output); err != nil {
		t.Fatal(err)
	}

	// Verify all queued
	if len(proc.messageQueue) != 3 {
		t.Fatalf("expected 3 messages in queue, got %d", len(proc.messageQueue))
	}

	// Signal to create inbox subscription
	if err := sendInboxMessage(proc, "control", payload.Payloads{payload.NewString("go")}, &output); err != nil {
		t.Fatal(err)
	}

	if err := inboxRunUntilDone(t, proc, 50); err != nil {
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

	if err := inboxRunUntilIdle(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	var output process.StepOutput

	// Send message BEFORE inbox subscription
	if err := sendInboxMessage(proc, "early_topic", payload.Payloads{payload.NewString("early")}, &output); err != nil {
		t.Fatal(err)
	}

	// Verify queued
	if len(proc.messageQueue) != 1 {
		t.Fatalf("expected 1 message in queue before inbox, got %d", len(proc.messageQueue))
	}

	// Signal phase 1 complete - this creates inbox subscription
	if err := sendInboxMessage(proc, "control", payload.Payloads{payload.NewString("phase1")}, &output); err != nil {
		t.Fatal(err)
	}

	// Run until idle again (waiting on second control:receive)
	if err := inboxRunUntilIdle(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	// Verify queue was flushed by inbox subscription
	if len(proc.messageQueue) != 0 {
		t.Errorf("queue should be empty after inbox subscription, got %d", len(proc.messageQueue))
	}

	// Send message AFTER inbox subscription exists
	if err := sendInboxMessage(proc, "late_topic", payload.Payloads{payload.NewString("late")}, &output); err != nil {
		t.Fatal(err)
	}

	// Signal phase 2 complete
	if err := sendInboxMessage(proc, "control", payload.Payloads{payload.NewString("phase2")}, &output); err != nil {
		t.Fatal(err)
	}

	if err := inboxRunUntilDone(t, proc, 50); err != nil {
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

	if err := inboxRunUntilIdle(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	var output process.StepOutput

	// Send @ topic message before inbox
	if err := sendInboxMessage(proc, "@system/test", payload.Payloads{payload.NewString("system")}, &output); err != nil {
		t.Fatal(err)
	}

	// Verify queued (won't be delivered to inbox because @ prefix)
	if len(proc.messageQueue) != 1 {
		t.Fatalf("@ topic should be queued, got %d", len(proc.messageQueue))
	}

	// Signal to create inbox
	if err := sendInboxMessage(proc, "control", payload.Payloads{payload.NewString("go")}, &output); err != nil {
		t.Fatal(err)
	}

	if err := inboxRunUntilDone(t, proc, 50); err != nil {
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

	if err := inboxRunUntilIdle(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	var output process.StepOutput

	// Send to "my_topic" before subscription exists
	if err := sendInboxMessage(proc, "my_topic", payload.Payloads{payload.NewString("early_value")}, &output); err != nil {
		t.Fatal(err)
	}

	// Verify queued
	if len(proc.messageQueue) != 1 {
		t.Fatalf("message should be queued, got %d", len(proc.messageQueue))
	}

	// Signal to create "my_topic" subscription
	if err := sendInboxMessage(proc, "control", payload.Payloads{payload.NewString("go")}, &output); err != nil {
		t.Fatal(err)
	}

	if err := inboxRunUntilDone(t, proc, 50); err != nil {
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
		Payloads: payload.Payloads{payload.NewString("pre_injected")},
	})

	// Now run - inbox subscription should flush the pre-injected message
	if err := inboxRunUntilDone(t, proc, 50); err != nil {
		t.Fatalf("Process failed: %v", err)
	}
}
