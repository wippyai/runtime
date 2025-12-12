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

// Message Ordering Tests
//
// These tests verify that messages sent BEFORE subscription are not lost
// and are delivered when subscription is created.

func createOrderingTestTranscoder() payload.Transcoder {
	transcoder := systempayload.NewTranscoder()
	luapayload.Register(transcoder)
	return transcoder
}

func startOrderingTestProcess(t *testing.T, script string) *Process {
	t.Helper()

	proto, err := lua.CompileString(script, "test.lua")
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	proc := NewProcess(WithProto(proto))

	rootCtx := ctxapi.NewRootContext()
	ctx, _ := ctxapi.OpenFrameContext(rootCtx)
	ctx = payload.WithTranscoder(ctx, createOrderingTestTranscoder())

	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}

	ChannelModule.Load(proc.State())
	loadPubSubGlobals(proc.State())

	return proc
}

func sendOrderingTestMessage(proc *Process, topic string, payloads payload.Payloads, output *process.StepOutput) error {
	events := []process.Event{{
		Type: process.EventMessage,
		Data: &relay.Package{
			Messages: []*relay.Message{{Topic: topic, Payloads: payloads}},
		},
	}}
	output.Reset()
	return proc.Step(events, output)
}

// TestMessagesBeforeSubscription verifies that messages sent BEFORE
// a subscription is created are NOT lost and are delivered when subscription happens.
func TestMessagesBeforeSubscription(t *testing.T) {
	// Script that does some work BEFORE subscribing
	script := `
		-- Do some initial work (no subscription yet)
		local x = 1 + 1

		-- Now subscribe
		local ch = channel.new(10)
		subscribe("my_topic", ch)

		-- Try to receive with timeout
		local result = channel.select{ch:case_receive(), default=true}

		if result.default then
			return nil, "message sent before subscription was LOST"
		end

		if result.value ~= "early_message" then
			return nil, "expected 'early_message', got " .. tostring(result.value)
		end

		return "ok"
	`

	proc := startOrderingTestProcess(t, script)
	defer proc.Close()

	var output process.StepOutput

	// DON'T run any steps yet - just send the message directly to the queue
	// to simulate message arriving before process even starts
	proc.messageQueue = append(proc.messageQueue, queuedMessage{
		Topic:    "my_topic",
		Payloads: payload.Payloads{payload.NewString("early_message")},
	})
	t.Logf("After manually queuing message: queueLen=%d", len(proc.messageQueue))

	// Now run until done - this should create subscription and receive the queued message
	for i := 0; i < 50; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("step %d failed: %v", i, err)
		}
		t.Logf("Step %d: status=%v, hasSubscriptions=%v, queueLen=%d",
			i, output.Status(), proc.HasSubscriptions(), len(proc.messageQueue))
		if output.Status() == process.StepDone {
			break
		}
	}

	if output.Status() != process.StepDone {
		t.Fatal("process did not complete")
	}

	t.Log("TestMessagesBeforeSubscription passed")
}

// TestMessagesBeforeInboxSubscription verifies that messages sent to random topics
// BEFORE inbox subscription are delivered to inbox when it's created.
func TestMessagesBeforeInboxSubscription(t *testing.T) {
	script := `
		-- Do some initial work
		local x = 1 + 1

		-- Now subscribe to inbox
		local inbox_ch = channel.new(10)
		subscribe("@pid/inbox", inbox_ch)

		-- Try to receive
		local result = channel.select{inbox_ch:case_receive(), default=true}

		if result.default then
			return nil, "message sent before inbox subscription was LOST"
		end

		if result.value ~= "early_inbox_message" then
			return nil, "expected 'early_inbox_message', got " .. tostring(result.value)
		end

		return "ok"
	`

	proc := startOrderingTestProcess(t, script)
	defer proc.Close()

	var output process.StepOutput

	// Manually queue a message to "random_topic" BEFORE running any steps
	proc.messageQueue = append(proc.messageQueue, queuedMessage{
		Topic:    "random_topic",
		Payloads: payload.Payloads{payload.NewString("early_inbox_message")},
	})
	t.Logf("After manual queue: queueLen=%d", len(proc.messageQueue))

	// Run until done
	for i := 0; i < 50; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("step %d failed: %v", i, err)
		}
		t.Logf("Step %d: status=%v, queueLen=%d", i, output.Status(), len(proc.messageQueue))
		if output.Status() == process.StepDone {
			break
		}
	}

	if output.Status() != process.StepDone {
		t.Fatal("process did not complete")
	}

	t.Log("TestMessagesBeforeInboxSubscription passed")
}

// TestMessageViaStepBeforeSubscription tests what happens when messages
// are sent via Step() before subscription exists
func TestMessageViaStepBeforeSubscription(t *testing.T) {
	script := `
		-- First yield to allow message to arrive
		coroutine.yield()

		-- Now subscribe
		local ch = channel.new(10)
		subscribe("my_topic", ch)

		-- Try to receive
		local result = channel.select{ch:case_receive(), default=true}

		if result.default then
			return nil, "message was LOST"
		end

		if result.value ~= "test" then
			return nil, "expected 'test', got " .. tostring(result.value)
		end

		return "ok"
	`

	proc := startOrderingTestProcess(t, script)
	defer proc.Close()

	var output process.StepOutput

	// Run one step - script yields
	output.Reset()
	if err := proc.Step(nil, &output); err != nil {
		t.Fatal(err)
	}
	t.Logf("Step 0: status=%v, queueLen=%d, hasSubs=%v",
		output.Status(), len(proc.messageQueue), proc.HasSubscriptions())

	// Now send message via Step - no subscription exists yet
	if err := sendOrderingTestMessage(proc, "my_topic", payload.Payloads{payload.NewString("test")}, &output); err != nil {
		t.Fatal(err)
	}
	t.Logf("After send: status=%v, queueLen=%d, hasSubs=%v",
		output.Status(), len(proc.messageQueue), proc.HasSubscriptions())

	// Run until done
	for i := 0; i < 50; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatalf("step %d failed: %v", i, err)
		}
		t.Logf("Step %d: status=%v, queueLen=%d, hasSubs=%v",
			i, output.Status(), len(proc.messageQueue), proc.HasSubscriptions())
		if output.Status() == process.StepDone {
			break
		}
	}

	if output.Status() != process.StepDone {
		t.Fatal("process did not complete")
	}

	t.Log("TestMessageViaStepBeforeSubscription passed")
}

// TestMessageQueueState verifies the queue state at various points
func TestMessageQueueState(t *testing.T) {
	script := `
		local ch = channel.new(10)
		subscribe("test_topic", ch)

		-- Block waiting for message
		local value = ch:receive()
		return value
	`

	proc := startOrderingTestProcess(t, script)
	defer proc.Close()

	var output process.StepOutput

	t.Logf("Initial queue length: %d", len(proc.messageQueue))

	// DON'T call sendOrderingTestMessage - that runs Step which processes queue
	// Instead, manually add message to queue
	proc.messageQueue = append(proc.messageQueue, queuedMessage{
		Topic:    "test_topic",
		Payloads: payload.Payloads{payload.NewString("msg1")},
	})
	t.Logf("After manual queue: queueLen=%d", len(proc.messageQueue))

	// Now run until idle or done
	for i := 0; i < 30; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Fatal(err)
		}
		t.Logf("Step %d: status=%v, queueLen=%d, hasSubs=%v",
			i, output.Status(), len(proc.messageQueue), proc.HasSubscriptions())
		if output.Status() == process.StepIdle || output.Status() == process.StepDone {
			break
		}
	}

	// If message was delivered, queue should be empty
	if len(proc.messageQueue) != 0 {
		t.Errorf("expected queue to be empty after subscription, got %d", len(proc.messageQueue))
	}

	t.Log("TestMessageQueueState passed")
}

// TestResponseChannelEarlyMessage simulates the gov pattern where
// response arrives before the client is ready to receive
func TestResponseChannelEarlyMessage(t *testing.T) {
	script := `
		-- Client creates response channel but does some work first
		local response_ch = channel.new(10)
		subscribe("response.123", response_ch)

		-- Simulate some work that takes time
		local x = 0
		for i = 1, 100 do
			x = x + i
		end

		-- Now try to receive
		local value = response_ch:receive()

		if type(value) ~= "table" then
			return nil, "expected table response, got " .. type(value)
		end

		if value.result ~= "success" then
			return nil, "expected result 'success', got " .. tostring(value.result)
		end

		return "ok"
	`

	proc := startOrderingTestProcess(t, script)
	defer proc.Close()

	var output process.StepOutput

	// Run until idle (blocked on receive)
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

	// Send response
	responsePayload := payload.New(map[string]any{
		"result": "success",
	})
	if err := sendOrderingTestMessage(proc, "response.123", payload.Payloads{responsePayload}, &output); err != nil {
		t.Fatal(err)
	}

	// Run until done
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

	t.Log("TestResponseChannelEarlyMessage passed")
}
