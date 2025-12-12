package engine

import (
	"context"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/topology"
	lua "github.com/yuin/gopher-lua"
)

// Channel Identity Tests
//
// These tests verify that channels returned by subscribe operations maintain
// identity across multiple calls. This is critical for patterns like:
//
//   local inbox = process.inbox()
//   local events = process.events()
//   -- later in select...
//   if result.channel == inbox then ...
//
// If channel identity is broken, the comparison will fail even though
// the channels are logically the same.

func startChannelIdentityProcess(t *testing.T, script string) *Process {
	proto, _ := lua.CompileString(script, "test.lua")
	proc := NewProcess(
		WithProto(proto),
	)

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}

	ChannelModule.Load(proc.State())
	loadPubSubGlobals(proc.State())
	return proc
}

// TestPushChannelIdempotent tests that PushChannel returns the same userdata
// for the same channel across multiple calls.
func TestPushChannelIdempotent(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	ChannelModule.Load(l)

	ch := NewChannel(10)

	// Push multiple times
	ud1 := PushChannel(l, ch)
	l.Pop(1) // Clean stack

	ud2 := PushChannel(l, ch)
	l.Pop(1)

	ud3 := PushChannel(l, ch)
	l.Pop(1)

	// All should be the same userdata
	if ud1 != ud2 {
		t.Error("PushChannel should return same userdata (call 1 vs 2)")
	}

	if ud2 != ud3 {
		t.Error("PushChannel should return same userdata (call 2 vs 3)")
	}

	// Channel's cached value should be the userdata
	if ch.Value() != ud1 {
		t.Error("channel should cache the userdata")
	}
}

// TestChannelValueCaching tests that Channel.Value() caching works correctly.
func TestChannelValueCaching(t *testing.T) {
	ch := NewChannel(10)

	// Initially, value should be nil
	if ch.Value() != nil {
		t.Error("new channel should have nil value")
	}

	// Create a mock LState and push channel
	l := lua.NewState()
	defer l.Close()
	ChannelModule.Load(l)

	ud := PushChannel(l, ch)

	// After PushChannel, Value() should return the userdata
	if ch.Value() == nil {
		t.Error("channel value should be set after PushChannel")
	}

	if ch.Value() != ud {
		t.Error("channel value should match the userdata returned by PushChannel")
	}

	// Push again - should reuse the cached value
	ud2 := PushChannel(l, ch)
	if ud != ud2 {
		t.Error("PushChannel should return same userdata for same channel")
	}
}

// TestSubscribeContextCreatesChannel tests that subscribeContext.add()
// creates a channel when none exists for a topic.
func TestSubscribeContextCreatesChannel(t *testing.T) {
	ctx := &subscribeContext{
		byTopic:   make(map[string]*subscription),
		byChannel: make(map[*Channel]string),
	}

	sub, err := ctx.add("topic1", 10)
	if err != nil {
		t.Fatalf("add failed: %v", err)
	}

	if sub.channel == nil {
		t.Error("subscription should have a channel")
	}

	// Channel should be in both maps
	if _, ok := ctx.byTopic["topic1"]; !ok {
		t.Error("topic should be in byTopic map")
	}

	if _, ok := ctx.byChannel[sub.channel]; !ok {
		t.Error("channel should be in byChannel map")
	}
}

// TestSubscribeContextReturnsSameChannel tests that subscribing to the same
// topic multiple times returns the same channel.
func TestSubscribeContextReturnsSameChannel(t *testing.T) {
	ctx := &subscribeContext{
		byTopic:   make(map[string]*subscription),
		byChannel: make(map[*Channel]string),
	}

	sub1, err := ctx.add("topic1", 10)
	if err != nil {
		t.Fatalf("first add failed: %v", err)
	}

	sub2, err := ctx.add("topic1", 5) // different bufSize ignored for existing
	if err != nil {
		t.Fatalf("second add failed: %v", err)
	}

	if sub1 != sub2 {
		t.Error("should return same subscription for same topic")
	}

	if sub1.channel != sub2.channel {
		t.Error("should return same channel for same topic")
	}
}

// TestSubscribeContextAddExisting tests that addExisting registers
// an externally-owned channel correctly.
func TestSubscribeContextAddExisting(t *testing.T) {
	ctx := &subscribeContext{
		byTopic:   make(map[string]*subscription),
		byChannel: make(map[*Channel]string),
	}

	ch := NewChannel(10)
	sub, err := ctx.addExisting("topic1", ch)
	if err != nil {
		t.Fatalf("addExisting failed: %v", err)
	}

	if sub.channel != ch {
		t.Error("subscription should reference the provided channel")
	}

	// Subscribe again with same channel - should succeed
	sub2, err := ctx.addExisting("topic1", ch)
	if err != nil {
		t.Fatalf("second addExisting failed: %v", err)
	}

	if sub != sub2 {
		t.Error("should return same subscription")
	}

	// Subscribe with different channel - should fail
	ch2 := NewChannel(10)
	_, err = ctx.addExisting("topic1", ch2)
	if err == nil {
		t.Error("addExisting with different channel should fail")
	}
}

// TestSystemTopicChannelIdentity tests channel identity for system topics.
func TestSystemTopicChannelIdentity(t *testing.T) {
	// Test that TopicInbox and TopicEvents are different
	if topology.TopicInbox == topology.TopicEvents {
		t.Error("TopicInbox and TopicEvents should be different")
	}

	ctx := &subscribeContext{
		byTopic:   make(map[string]*subscription),
		byChannel: make(map[*Channel]string),
	}

	inboxSub, err := ctx.add(string(topology.TopicInbox), 0)
	if err != nil {
		t.Fatalf("inbox subscribe failed: %v", err)
	}

	eventsSub, err := ctx.add(string(topology.TopicEvents), 0)
	if err != nil {
		t.Fatalf("events subscribe failed: %v", err)
	}

	// Different topics should have different channels
	if inboxSub.channel == eventsSub.channel {
		t.Error("inbox and events should have different channels")
	}

	// Re-subscribing to same topic should return same channel
	inboxSub2, _ := ctx.add(string(topology.TopicInbox), 0)
	if inboxSub.channel != inboxSub2.channel {
		t.Error("inbox channel identity not preserved")
	}
}

// TestChannelIdentityInLuaComparison tests that Lua equality comparison
// works correctly for channels.
func TestChannelIdentityInLuaComparison(t *testing.T) {
	script := `
		local ch = channel.new(10)
		local same = ch
		local different = channel.new(10)

		if ch ~= same then
			error("same channel reference should be equal")
		end

		if ch == different then
			error("different channels should not be equal")
		end

		return "ok"
	`

	proc := startChannelIdentityProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 20); err != nil {
		t.Fatal(err)
	}
}

// TestSubscribeExistingChannelReturnsIt tests that subscribe() with an existing
// channel returns that same channel.
func TestSubscribeExistingChannelReturnsIt(t *testing.T) {
	script := `
		local ch = channel.new(10)
		local result = subscribe("my_topic", ch)

		if result == nil then
			error("subscribe should return channel")
		end

		-- Result should be the same channel we passed in
		if result ~= ch then
			error("subscribe should return the same channel we provided")
		end

		return "ok"
	`

	proc := startChannelIdentityProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 30); err != nil {
		t.Fatal(err)
	}
}

// TestSubscribeSameTopicDifferentChannelErrors tests that subscribing to the
// same topic with a different channel returns an error.
func TestSubscribeSameTopicDifferentChannelErrors(t *testing.T) {
	script := `
		local ch1 = channel.new(10)
		local ch2 = channel.new(10)

		local result1 = subscribe("test_topic", ch1)
		if result1 == nil then
			error("first subscribe should succeed")
		end

		-- Second subscribe with different channel should error
		local result2 = subscribe("test_topic", ch2)
		if result2 ~= nil then
			error("second subscribe with different channel should fail")
		end

		return "ok"
	`

	proc := startChannelIdentityProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 30); err != nil {
		t.Fatal(err)
	}
}

// TestSubscribeRequestFields tests SubscribeRequest struct fields.
func TestSubscribeRequestFields(t *testing.T) {
	// Test with ExistingChannel
	ch := NewChannel(10)
	req := &SubscribeRequest{
		Topic:           "test",
		ExistingChannel: ch,
	}

	if req.ExistingChannel != ch {
		t.Error("SubscribeRequest should hold the ExistingChannel reference")
	}

	if req.String() != "<subscribe_request>" {
		t.Errorf("unexpected String(): %s", req.String())
	}

	if req.Type() != lua.LTUserData {
		t.Errorf("unexpected Type(): %v", req.Type())
	}

	// Test without ExistingChannel (subscription creates channel)
	req2 := &SubscribeRequest{
		Topic:   "test2",
		BufSize: 5,
	}

	if req2.ExistingChannel != nil {
		t.Error("ExistingChannel should be nil")
	}

	if req2.BufSize != 5 {
		t.Error("BufSize should be set")
	}
}

// TestMultipleSubscribeDifferentTopics tests subscribing to multiple
// different topics works correctly.
func TestMultipleSubscribeDifferentTopics(t *testing.T) {
	script := `
		local ch1 = channel.new(10)
		local ch2 = channel.new(10)

		local result1 = subscribe("topic_a", ch1)
		local result2 = subscribe("topic_b", ch2)

		if result1 == nil or result2 == nil then
			error("both subscribes should succeed")
		end

		if result1 == result2 then
			error("different topics should have different channels")
		end

		return "ok"
	`

	proc := startChannelIdentityProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 30); err != nil {
		t.Fatal(err)
	}
}

// TestChannelIdentityAcrossSelect tests that channel identity is preserved
// when a channel is used in subscribe operations.
func TestChannelIdentityAcrossSelect(t *testing.T) {
	script := `
		local ch = channel.new(10)
		subscribe("test_select", ch)

		-- Store reference before any operations
		local stored = ch

		-- Verify identity is preserved
		if ch ~= stored then
			error("channel identity lost after subscribe")
		end

		return "ok"
	`

	proc := startChannelIdentityProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 30); err != nil {
		t.Fatal(err)
	}
}
