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

// Inbox Queue Flush Behavior Tests
//
// These tests verify that messages sent BEFORE inbox subscription exists
// are properly queued and delivered when the inbox subscription is created.
//
// This is critical for scenarios where:
// 1. A parent process spawns a child and immediately sends a message
// 2. The child's inbox subscription is created after the message arrives
// 3. The message must still be delivered via inbox fallback
//
// Expected behavior:
// - Messages to non-@ topics without a specific listener get queued
// - When inbox subscription is created, flushMessageQueue delivers queued messages
// - Messages that arrived before subscription should be delivered in order

func startQueueFlushProcess(t *testing.T, script string) *Process {
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

func runQueueFlushUntilIdle(t *testing.T, proc *Process, maxSteps int) error {
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

func runQueueFlushUntilDone(t *testing.T, proc *Process, maxSteps int) error {
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

func sendQueueFlushMessage(proc *Process, topic string, payloads payload.Payloads, output *process.StepOutput) error {
	events := []process.Event{{
		Type: process.EventMessage,
		Data: &relay.Package{
			Messages: []*relay.Message{{Topic: topic, Payloads: payloads}},
		},
	}}
	output.Reset()
	return proc.Step(events, output)
}

// TestMessageQueuedBeforeInboxSubscription verifies that messages sent to
// non-@ topics before inbox subscription exists are queued and delivered
// when inbox subscription is created.
func TestMessageQueuedBeforeInboxSubscription(t *testing.T) {
	// Script that creates inbox subscription AFTER yielding once
	// This allows us to send a message before inbox exists
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

	proc := startQueueFlushProcess(t, script)
	defer proc.Close()

	// Run until idle (blocked on control:receive)
	if err := runQueueFlushUntilIdle(t, proc, 30); err != nil {
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
	if err := sendQueueFlushMessage(proc, "my_topic", payload.Payloads{payload.NewString("early_message")}, &output); err != nil {
		t.Fatalf("send message failed: %v", err)
	}

	// Verify message is in queue (not delivered because no inbox)
	if len(proc.messageQueue) != 1 {
		t.Fatalf("message should be queued, queue len: %d", len(proc.messageQueue))
	}

	// Signal control to continue (this will create inbox subscription)
	if err := sendQueueFlushMessage(proc, "control", payload.Payloads{payload.NewString("go")}, &output); err != nil {
		t.Fatalf("send control failed: %v", err)
	}

	// Run until done
	if err := runQueueFlushUntilDone(t, proc, 50); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Verify queue was flushed
	if len(proc.messageQueue) != 0 {
		t.Errorf("message queue should be empty after inbox subscription, len: %d", len(proc.messageQueue))
	}

	t.Log("TestMessageQueuedBeforeInboxSubscription passed")
}

// TestMultipleMessagesQueuedBeforeInboxSubscription verifies that multiple
// messages queued before inbox subscription are all delivered in order.
func TestMultipleMessagesQueuedBeforeInboxSubscription(t *testing.T) {
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

	proc := startQueueFlushProcess(t, script)
	defer proc.Close()

	if err := runQueueFlushUntilIdle(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	var output process.StepOutput

	// Send multiple messages before inbox subscription
	if err := sendQueueFlushMessage(proc, "topic_a", payload.Payloads{payload.NewString("msg1")}, &output); err != nil {
		t.Fatal(err)
	}
	if err := sendQueueFlushMessage(proc, "topic_b", payload.Payloads{payload.NewString("msg2")}, &output); err != nil {
		t.Fatal(err)
	}
	if err := sendQueueFlushMessage(proc, "topic_c", payload.Payloads{payload.NewString("msg3")}, &output); err != nil {
		t.Fatal(err)
	}

	// Verify all queued
	if len(proc.messageQueue) != 3 {
		t.Fatalf("expected 3 messages in queue, got %d", len(proc.messageQueue))
	}

	// Signal to create inbox subscription
	if err := sendQueueFlushMessage(proc, "control", payload.Payloads{payload.NewString("go")}, &output); err != nil {
		t.Fatal(err)
	}

	if err := runQueueFlushUntilDone(t, proc, 50); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	t.Log("TestMultipleMessagesQueuedBeforeInboxSubscription passed")
}

// TestMixedMessageDeliveryBeforeAndAfterSubscription verifies correct behavior
// when some messages arrive before inbox subscription and some after.
func TestMixedMessageDeliveryBeforeAndAfterSubscription(t *testing.T) {
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

	proc := startQueueFlushProcess(t, script)
	defer proc.Close()

	if err := runQueueFlushUntilIdle(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	var output process.StepOutput

	// Send message BEFORE inbox subscription
	if err := sendQueueFlushMessage(proc, "early_topic", payload.Payloads{payload.NewString("early")}, &output); err != nil {
		t.Fatal(err)
	}

	// Verify queued
	if len(proc.messageQueue) != 1 {
		t.Fatalf("expected 1 message in queue before inbox, got %d", len(proc.messageQueue))
	}

	// Signal phase 1 complete - this creates inbox subscription
	if err := sendQueueFlushMessage(proc, "control", payload.Payloads{payload.NewString("phase1")}, &output); err != nil {
		t.Fatal(err)
	}

	// Run until idle again (waiting on second control:receive)
	if err := runQueueFlushUntilIdle(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	// Verify queue was flushed by inbox subscription
	if len(proc.messageQueue) != 0 {
		t.Errorf("queue should be empty after inbox subscription, got %d", len(proc.messageQueue))
	}

	// Send message AFTER inbox subscription exists
	if err := sendQueueFlushMessage(proc, "late_topic", payload.Payloads{payload.NewString("late")}, &output); err != nil {
		t.Fatal(err)
	}

	// Signal phase 2 complete
	if err := sendQueueFlushMessage(proc, "control", payload.Payloads{payload.NewString("phase2")}, &output); err != nil {
		t.Fatal(err)
	}

	if err := runQueueFlushUntilDone(t, proc, 50); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	t.Log("TestMixedMessageDeliveryBeforeAndAfterSubscription passed")
}

// TestSystemTopicNotQueuedForInbox verifies that @ topics are NOT delivered
// to inbox even when queued before inbox subscription exists.
func TestSystemTopicNotQueuedForInbox(t *testing.T) {
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

	proc := startQueueFlushProcess(t, script)
	defer proc.Close()

	if err := runQueueFlushUntilIdle(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	var output process.StepOutput

	// Send @ topic message before inbox
	if err := sendQueueFlushMessage(proc, "@system/test", payload.Payloads{payload.NewString("system")}, &output); err != nil {
		t.Fatal(err)
	}

	// Verify queued (won't be delivered to inbox because @ prefix)
	if len(proc.messageQueue) != 1 {
		t.Fatalf("@ topic should be queued, got %d", len(proc.messageQueue))
	}

	// Signal to create inbox
	if err := sendQueueFlushMessage(proc, "control", payload.Payloads{payload.NewString("go")}, &output); err != nil {
		t.Fatal(err)
	}

	if err := runQueueFlushUntilDone(t, proc, 50); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// @ topic should STILL be in queue (not delivered to inbox)
	if len(proc.messageQueue) != 1 {
		t.Errorf("@ topic should remain in queue, got %d", len(proc.messageQueue))
	}

	t.Log("TestSystemTopicNotQueuedForInbox passed")
}

// TestQueueFlushOnEachSubscription verifies that queue is flushed each time
// a new subscription is created (not just inbox).
func TestQueueFlushOnEachSubscription(t *testing.T) {
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

	proc := startQueueFlushProcess(t, script)
	defer proc.Close()

	if err := runQueueFlushUntilIdle(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	var output process.StepOutput

	// Send to "my_topic" before subscription exists
	if err := sendQueueFlushMessage(proc, "my_topic", payload.Payloads{payload.NewString("early_value")}, &output); err != nil {
		t.Fatal(err)
	}

	// Verify queued
	if len(proc.messageQueue) != 1 {
		t.Fatalf("message should be queued, got %d", len(proc.messageQueue))
	}

	// Signal to create "my_topic" subscription
	if err := sendQueueFlushMessage(proc, "control", payload.Payloads{payload.NewString("go")}, &output); err != nil {
		t.Fatal(err)
	}

	if err := runQueueFlushUntilDone(t, proc, 50); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	t.Log("TestQueueFlushOnEachSubscription passed")
}

// TestDirectQueueInjectionBeforeSubscription tests using direct queue manipulation
// to simulate a race condition where message arrives before any execution.
func TestDirectQueueInjectionBeforeSubscription(t *testing.T) {
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

	proc := startQueueFlushProcess(t, script)
	defer proc.Close()

	// Directly inject a message into the queue BEFORE any step execution
	proc.messageQueue = append(proc.messageQueue, queuedMessage{
		Topic:    "injected_topic",
		Payloads: payload.Payloads{payload.NewString("pre_injected")},
	})

	// Now run - inbox subscription should flush the pre-injected message
	if err := runQueueFlushUntilDone(t, proc, 50); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	t.Log("TestDirectQueueInjectionBeforeSubscription passed")
}
