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

// Note: Tests use the Lua error-return pattern (return nil, "error message")
// to validate results within the Lua script. If the script returns an error,
// proc.execErr will be set and runInboxTestUntilDone will return it.

// Inbox Fallback Behavior Tests
//
// These tests verify that inbox only receives messages that don't match
// any specific listener topic.
//
// Expected behavior:
// 1. Messages to topics with listeners go to those listeners only
// 2. Messages to topics without listeners fall through to inbox
// 3. System topics (starting with @) do NOT fall back to inbox
// 4. Inbox acts as catch-all for unmatched user topics

func startInboxTestProcess(t *testing.T, script string) *Process {
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

func runInboxTestUntilIdle(t *testing.T, proc *Process, maxSteps int) error {
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

func runInboxTestUntilDone(t *testing.T, proc *Process, maxSteps int) error {
	t.Helper()
	var output process.StepOutput
	for i := 0; i < maxSteps; i++ {
		output.Reset()
		if err := proc.Step(nil, &output); err != nil {
			t.Logf("step %d error: %v", i, err)
			return err
		}
		t.Logf("step %d status: %v", i, output.Status())
		if output.Status() == process.StepDone {
			return nil
		}
	}
	t.Fatalf("did not reach done in %d steps", maxSteps)
	return nil
}

func sendTestMessage(proc *Process, topic string, payloads payload.Payloads, output *process.StepOutput) error {
	events := []process.Event{{
		Type: process.EventMessage,
		Data: &relay.Package{
			Messages: []*relay.Message{{Topic: topic, Payloads: payloads}},
		},
	}}
	output.Reset()
	return proc.Step(events, output)
}

// TestInboxFallbackBehavior verifies messages to matched topics go to listeners,
// unmatched topics fall through to inbox.
func TestInboxFallbackBehavior(t *testing.T) {
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

	proc := startInboxTestProcess(t, script)
	defer proc.Close()

	if err := runInboxTestUntilIdle(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	if !proc.HasSubscriptions() {
		t.Error("expected subscriptions to be active")
	}

	var output process.StepOutput

	// Message to specific_topic (should go to specific listener, NOT inbox)
	if err := sendTestMessage(proc, "specific_topic", payload.Payloads{payload.NewString("msg1")}, &output); err != nil {
		t.Fatalf("send to specific_topic failed: %v", err)
	}

	// Message to unmatched topic (should fall through to inbox)
	if err := sendTestMessage(proc, "random_topic", payload.Payloads{payload.NewString("msg2")}, &output); err != nil {
		t.Fatalf("send to random_topic failed: %v", err)
	}

	// Another message to specific_topic
	if err := sendTestMessage(proc, "specific_topic", payload.Payloads{payload.NewString("msg3")}, &output); err != nil {
		t.Fatalf("send second to specific_topic failed: %v", err)
	}

	// Signal to start counting
	if err := sendTestMessage(proc, "control", payload.Payloads{payload.NewString("go")}, &output); err != nil {
		t.Fatalf("send control failed: %v", err)
	}

	if err := runInboxTestUntilDone(t, proc, 50); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	t.Log("TestInboxFallbackBehavior passed")
}

// TestInboxDoesNotReceiveMatchedTopics verifies that when a specific topic
// listener exists, messages to that topic do NOT also go to inbox.
func TestInboxDoesNotReceiveMatchedTopics(t *testing.T) {
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

	proc := startInboxTestProcess(t, script)
	defer proc.Close()

	if err := runInboxTestUntilIdle(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	// Send ONLY to the topic that has a listener
	var output process.StepOutput
	if err := sendTestMessage(proc, "my_topic", payload.Payloads{payload.NewString("test")}, &output); err != nil {
		t.Fatal(err)
	}

	// Signal to start counting
	if err := sendTestMessage(proc, "control", payload.Payloads{payload.NewString("go")}, &output); err != nil {
		t.Fatal(err)
	}

	if err := runInboxTestUntilDone(t, proc, 50); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	t.Log("TestInboxDoesNotReceiveMatchedTopics passed")
}

// TestInboxReceivesUnmatchedTopics verifies that messages to topics without
// specific listeners fall through to inbox.
func TestInboxReceivesUnmatchedTopics(t *testing.T) {
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

	proc := startInboxTestProcess(t, script)
	defer proc.Close()

	if err := runInboxTestUntilIdle(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	// Send to random topics (none have listeners)
	var output process.StepOutput
	if err := sendTestMessage(proc, "topic_a", payload.Payloads{payload.NewString("a")}, &output); err != nil {
		t.Fatal(err)
	}
	if err := sendTestMessage(proc, "topic_b", payload.Payloads{payload.NewString("b")}, &output); err != nil {
		t.Fatal(err)
	}
	if err := sendTestMessage(proc, "topic_c", payload.Payloads{payload.NewString("c")}, &output); err != nil {
		t.Fatal(err)
	}

	// Signal to count
	if err := sendTestMessage(proc, "control", payload.Payloads{payload.NewString("go")}, &output); err != nil {
		t.Fatal(err)
	}

	if err := runInboxTestUntilDone(t, proc, 50); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	t.Log("TestInboxReceivesUnmatchedTopics passed")
}

// TestSystemTopicsDoNotFallbackToInbox verifies that @ topics (system topics)
// do NOT fall back to inbox when unmatched.
func TestSystemTopicsDoNotFallbackToInbox(t *testing.T) {
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

	proc := startInboxTestProcess(t, script)
	defer proc.Close()

	if err := runInboxTestUntilIdle(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	// Send to a system topic that doesn't have a listener
	var output process.StepOutput
	if err := sendTestMessage(proc, "@system/unknown", payload.Payloads{payload.NewString("test")}, &output); err != nil {
		t.Fatal(err)
	}

	// Signal to count
	if err := sendTestMessage(proc, "control", payload.Payloads{payload.NewString("go")}, &output); err != nil {
		t.Fatal(err)
	}

	if err := runInboxTestUntilDone(t, proc, 50); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Also verify message remained in queue (undelivered)
	if len(proc.messageQueue) != 1 {
		t.Errorf("@ topic message should remain in queue, queue len: %d", len(proc.messageQueue))
	}

	t.Log("TestSystemTopicsDoNotFallbackToInbox passed")
}

// TestMultipleListenersRoutingPriority verifies that the most specific listener
// gets the message, not inbox.
func TestMultipleListenersRoutingPriority(t *testing.T) {
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

	proc := startInboxTestProcess(t, script)
	defer proc.Close()

	if err := runInboxTestUntilIdle(t, proc, 30); err != nil {
		t.Fatal(err)
	}

	var output process.StepOutput
	// Send to topic_a
	if err := sendTestMessage(proc, "topic_a", payload.Payloads{payload.NewString("for_a")}, &output); err != nil {
		t.Fatal(err)
	}
	// Send to topic_b
	if err := sendTestMessage(proc, "topic_b", payload.Payloads{payload.NewString("for_b")}, &output); err != nil {
		t.Fatal(err)
	}
	// Send to unmatched topic
	if err := sendTestMessage(proc, "topic_c", payload.Payloads{payload.NewString("for_inbox")}, &output); err != nil {
		t.Fatal(err)
	}

	// Signal to count
	if err := sendTestMessage(proc, "control", payload.Payloads{payload.NewString("go")}, &output); err != nil {
		t.Fatal(err)
	}

	if err := runInboxTestUntilDone(t, proc, 50); err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	t.Log("TestMultipleListenersRoutingPriority passed")
}
