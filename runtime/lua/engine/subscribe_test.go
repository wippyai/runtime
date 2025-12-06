package engine

import (
	"testing"

	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lua "github.com/yuin/gopher-lua"
)

func TestSubscribeContext(t *testing.T) {
	ctx := &subscribeContext{
		byTopic:   make(map[string]*subscription),
		byChannel: make(map[*Channel]string),
	}

	ch1 := NewChannel(1)
	ch2 := NewChannel(1)

	// Test add
	sub, err := ctx.add("topic1", ch1)
	if err != nil {
		t.Fatalf("add failed: %v", err)
	}
	if sub.topic != "topic1" {
		t.Errorf("sub.topic = %q, want %q", sub.topic, "topic1")
	}
	if sub.channel != ch1 {
		t.Error("sub.channel should be ch1")
	}

	// Test add same channel to same topic (should succeed)
	sub2, err := ctx.add("topic1", ch1)
	if err != nil {
		t.Fatalf("add same channel to same topic should succeed: %v", err)
	}
	if sub2 != sub {
		t.Error("should return existing subscription")
	}

	// Test add different channel to same topic (should fail)
	_, err = ctx.add("topic1", ch2)
	if err == nil {
		t.Error("add different channel to same topic should fail")
	}

	// Test get
	gotSub, ok := ctx.get("topic1")
	if !ok {
		t.Error("get should find topic1")
	}
	if gotSub != sub {
		t.Error("get should return correct subscription")
	}

	_, ok = ctx.get("nonexistent")
	if ok {
		t.Error("get should return false for nonexistent topic")
	}

	// Test remove
	err = ctx.remove(ch1)
	if err != nil {
		t.Fatalf("remove failed: %v", err)
	}

	_, ok = ctx.get("topic1")
	if ok {
		t.Error("topic1 should be removed")
	}

	// Test remove non-subscribed channel
	err = ctx.remove(ch2)
	if err != luaapi.ErrChannelNotFound {
		t.Errorf("remove non-subscribed channel should return ErrChannelNotFound, got %v", err)
	}
}

func TestSubscribeContextConcurrentSafe(t *testing.T) {
	ctx := &subscribeContext{
		byTopic:   make(map[string]*subscription),
		byChannel: make(map[*Channel]string),
	}

	ch := NewChannel(1)
	_, err := ctx.add("topic", ch)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate concurrent read (get uses RLock)
	done := make(chan bool)
	go func() {
		for i := 0; i < 100; i++ {
			ctx.get("topic")
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			ctx.get("topic")
		}
		done <- true
	}()

	<-done
	<-done
}

func TestSubscribeContextMultipleTopics(t *testing.T) {
	ctx := &subscribeContext{
		byTopic:   make(map[string]*subscription),
		byChannel: make(map[*Channel]string),
	}

	ch1 := NewChannel(1)
	ch2 := NewChannel(1)
	ch3 := NewChannel(1)

	ctx.add("topic1", ch1)
	ctx.add("topic2", ch2)
	ctx.add("topic3", ch3)

	// Verify all exist
	if _, ok := ctx.get("topic1"); !ok {
		t.Error("topic1 should exist")
	}
	if _, ok := ctx.get("topic2"); !ok {
		t.Error("topic2 should exist")
	}
	if _, ok := ctx.get("topic3"); !ok {
		t.Error("topic3 should exist")
	}

	// Remove middle one
	ctx.remove(ch2)

	if _, ok := ctx.get("topic1"); !ok {
		t.Error("topic1 should still exist")
	}
	if _, ok := ctx.get("topic2"); ok {
		t.Error("topic2 should be removed")
	}
	if _, ok := ctx.get("topic3"); !ok {
		t.Error("topic3 should still exist")
	}
}

func TestSubscription(t *testing.T) {
	ch := NewChannel(1)
	sub := &subscription{
		topic:   "test-topic",
		channel: ch,
	}

	if sub.topic != "test-topic" {
		t.Errorf("topic = %q, want %q", sub.topic, "test-topic")
	}
	if sub.channel != ch {
		t.Error("channel mismatch")
	}
}

func TestSubscribeRequestString(t *testing.T) {
	ch := NewChannel(1)
	req := &SubscribeRequest{
		Topic:   "my-topic",
		Channel: ch,
	}

	if req.String() != "<subscribe_request>" {
		t.Errorf("String() = %q, want %q", req.String(), "<subscribe_request>")
	}
	if req.Type() != lua.LTUserData {
		t.Errorf("Type() = %v, want %v", req.Type(), lua.LTUserData)
	}
	if req.Topic != "my-topic" {
		t.Errorf("Topic = %q, want %q", req.Topic, "my-topic")
	}
	if req.Channel != ch {
		t.Error("Channel mismatch")
	}
}

func TestUnsubscribeRequestString(t *testing.T) {
	ch := NewChannel(1)
	req := &UnsubscribeRequest{
		Channel: ch,
	}

	if req.String() != "<unsubscribe_request>" {
		t.Errorf("String() = %q, want %q", req.String(), "<unsubscribe_request>")
	}
	if req.Type() != lua.LTUserData {
		t.Errorf("Type() = %v, want %v", req.Type(), lua.LTUserData)
	}
	if req.Channel != ch {
		t.Error("Channel mismatch")
	}
}

func TestSubscribeContextAddRemoveSequence(t *testing.T) {
	ctx := &subscribeContext{
		byTopic:   make(map[string]*subscription),
		byChannel: make(map[*Channel]string),
	}

	ch := NewChannel(1)

	// Add
	_, err := ctx.add("topic", ch)
	if err != nil {
		t.Fatal(err)
	}

	// Remove
	err = ctx.remove(ch)
	if err != nil {
		t.Fatal(err)
	}

	// Add again (resubscribe)
	_, err = ctx.add("topic", ch)
	if err != nil {
		t.Fatalf("resubscribe should work: %v", err)
	}

	// Different channel, same topic should fail now
	ch2 := NewChannel(1)
	_, err = ctx.add("topic", ch2)
	if err == nil {
		t.Error("adding different channel to occupied topic should fail")
	}
}

func TestSubscribeContextTopicChannelMapping(t *testing.T) {
	ctx := &subscribeContext{
		byTopic:   make(map[string]*subscription),
		byChannel: make(map[*Channel]string),
	}

	ch := NewChannel(1)
	ctx.add("my-topic", ch)

	// Check byChannel mapping
	if topic, ok := ctx.byChannel[ch]; !ok || topic != "my-topic" {
		t.Errorf("byChannel mapping incorrect: got %q", topic)
	}

	// Check byTopic mapping
	if sub, ok := ctx.byTopic["my-topic"]; !ok || sub.channel != ch {
		t.Error("byTopic mapping incorrect")
	}

	// After remove, both should be cleared
	ctx.remove(ch)

	if _, ok := ctx.byChannel[ch]; ok {
		t.Error("byChannel should be cleared after remove")
	}
	if _, ok := ctx.byTopic["my-topic"]; ok {
		t.Error("byTopic should be cleared after remove")
	}
}
