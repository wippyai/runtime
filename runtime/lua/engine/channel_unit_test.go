// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"testing"

	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/topology"
)

// Channel Unit Tests
//
// This file contains unit tests for Channel struct and subscribeContext:
// 1. Go channel semantics compliance (send/receive blocking, FIFO, close)
// 2. Channel identity and caching
// 3. Subscribe context management

func newUnitTestLState() *lua.LState {
	return lua.NewState()
}

func startChannelUnitProcess(t *testing.T, script string) *Process {
	proto, _ := lua.CompileString(script, "test.lua")
	proc := mustNewProcess(t, WithProto(proto))

	ctx, _ := ctxapi.OpenFrameContext(context.Background())
	if err := proc.Init(ctx, "", nil); err != nil {
		t.Fatal(err)
	}

	LoadModuleDef(proc.State(), ChannelModule)
	loadPubSubGlobals(proc.State())
	return proc
}

// ============================================================================
// UNBUFFERED CHANNEL TESTS
// ============================================================================

func TestChannel_UnbufferedSendBlocks(t *testing.T) {
	ch := NewChannel(0)
	sender := newUnitTestLState()

	result := ch.Send(sender, lua.LString("value"), nil)
	if !result.Yields {
		t.Error("send on unbuffered channel without receiver should yield/block")
	}
	if len(result.Block) != 1 || result.Block[0] != ch {
		t.Error("blocked send should report blocking on this channel")
	}
}

func TestChannel_UnbufferedReceiveBlocks(t *testing.T) {
	ch := NewChannel(0)
	recv := newUnitTestLState()

	result := ch.Receive(recv, nil)
	if !result.Yields {
		t.Error("receive on empty unbuffered channel should yield/block")
	}
	if len(result.Block) != 1 || result.Block[0] != ch {
		t.Error("blocked receive should report blocking on this channel")
	}
}

func TestChannel_UnbufferedDirectHandoff(t *testing.T) {
	ch := NewChannel(0)
	recv := newUnitTestLState()
	sender := newUnitTestLState()

	ch.Receive(recv, nil)
	sendResult := ch.Send(sender, lua.LString("hello"), nil)
	if !sendResult.Yields {
		t.Error("send that synchronizes with receiver should yield")
	}

	updates := sendResult.GetUpdates()
	if len(updates) != 2 {
		t.Fatalf("expected 2 updates (sender + receiver), got %d", len(updates))
	}

	var receiverUpdate, senderUpdate *TaskUpdate
	for _, u := range updates {
		switch u.State {
		case recv:
			receiverUpdate = u
		case sender:
			senderUpdate = u
		}
	}

	if receiverUpdate == nil {
		t.Fatal("missing receiver update")
	}
	if senderUpdate == nil {
		t.Fatal("missing sender update")
	}

	result := receiverUpdate.GetResult()
	if len(result) != 2 {
		t.Fatalf("receiver expected 2 results, got %d", len(result))
	}
	if result[0] != lua.LString("hello") {
		t.Errorf("receiver expected 'hello', got %v", result[0])
	}
	if result[1] != lua.LTrue {
		t.Errorf("receiver expected ok=true, got %v", result[1])
	}
}

func TestChannel_UnbufferedSenderWaitsForReceiver(t *testing.T) {
	ch := NewChannel(0)
	recv := newUnitTestLState()
	sender := newUnitTestLState()

	ch.Send(sender, lua.LString("world"), nil)
	recvResult := ch.Receive(recv, nil)
	if !recvResult.Yields {
		t.Error("receive that synchronizes with blocked sender should yield")
	}

	updates := recvResult.GetUpdates()
	if len(updates) != 2 {
		t.Fatalf("expected 2 updates (sender + receiver), got %d", len(updates))
	}

	var receiverUpdate *TaskUpdate
	for _, u := range updates {
		if u.State == recv {
			receiverUpdate = u
		}
	}

	if receiverUpdate == nil {
		t.Fatal("missing receiver update")
	}

	result := receiverUpdate.GetResult()
	if result[0] != lua.LString("world") {
		t.Errorf("receiver expected 'world', got %v", result[0])
	}
}

// ============================================================================
// BUFFERED CHANNEL TESTS
// ============================================================================

func TestChannel_BufferedSendDoesNotBlock(t *testing.T) {
	ch := NewChannel(2)
	sender := newUnitTestLState()

	r1 := ch.Send(sender, lua.LNumber(1), nil)
	if r1.Yields {
		t.Error("first buffered send should not block")
	}

	r2 := ch.Send(sender, lua.LNumber(2), nil)
	if r2.Yields {
		t.Error("second buffered send should not block")
	}
}

func TestChannel_BufferedSendBlocksWhenFull(t *testing.T) {
	ch := NewChannel(2)
	sender := newUnitTestLState()
	blocker := newUnitTestLState()

	ch.Send(sender, lua.LNumber(1), nil)
	ch.Send(sender, lua.LNumber(2), nil)

	r3 := ch.Send(blocker, lua.LNumber(3), nil)
	if !r3.Yields {
		t.Error("send to full buffer should block")
	}
	if len(r3.Block) != 1 || r3.Block[0] != ch {
		t.Error("blocked send should report blocking on channel")
	}
}

func TestChannel_BufferedReceiveDoesNotBlock(t *testing.T) {
	ch := NewChannel(2)
	sender := newUnitTestLState()
	recv := newUnitTestLState()

	ch.Send(sender, lua.LNumber(42), nil)
	r := ch.Receive(recv, nil)
	if r.Yields {
		t.Error("receive from non-empty buffer should not block")
	}

	updates := r.GetUpdates()
	if len(updates) != 1 {
		t.Fatalf("expected 1 update, got %d", len(updates))
	}

	result := updates[0].GetResult()
	if result[0] != lua.LNumber(42) {
		t.Errorf("expected 42, got %v", result[0])
	}
	if result[1] != lua.LTrue {
		t.Errorf("expected ok=true, got %v", result[1])
	}
}

func TestChannel_BufferedReceiveBlocksWhenEmpty(t *testing.T) {
	ch := NewChannel(2)
	recv := newUnitTestLState()

	r := ch.Receive(recv, nil)
	if !r.Yields {
		t.Error("receive from empty buffer should block")
	}
}

// ============================================================================
// FIFO ORDERING TESTS
// ============================================================================

func TestChannel_FIFOSenderOrder(t *testing.T) {
	ch := NewChannel(0)
	sender1 := newUnitTestLState()
	sender2 := newUnitTestLState()
	sender3 := newUnitTestLState()
	recv1 := newUnitTestLState()
	recv2 := newUnitTestLState()
	recv3 := newUnitTestLState()

	ch.Send(sender1, lua.LNumber(1), nil)
	ch.Send(sender2, lua.LNumber(2), nil)
	ch.Send(sender3, lua.LNumber(3), nil)

	r1 := ch.Receive(recv1, nil)
	var val1 lua.LValue
	for _, u := range r1.GetUpdates() {
		if u.State == recv1 {
			val1 = u.GetResult()[0]
		}
	}
	if val1 != lua.LNumber(1) {
		t.Errorf("first receive expected 1, got %v", val1)
	}

	r2 := ch.Receive(recv2, nil)
	var val2 lua.LValue
	for _, u := range r2.GetUpdates() {
		if u.State == recv2 {
			val2 = u.GetResult()[0]
		}
	}
	if val2 != lua.LNumber(2) {
		t.Errorf("second receive expected 2, got %v", val2)
	}

	r3 := ch.Receive(recv3, nil)
	var val3 lua.LValue
	for _, u := range r3.GetUpdates() {
		if u.State == recv3 {
			val3 = u.GetResult()[0]
		}
	}
	if val3 != lua.LNumber(3) {
		t.Errorf("third receive expected 3, got %v", val3)
	}
}

func TestChannel_FIFOReceiverOrder(t *testing.T) {
	ch := NewChannel(0)
	recv1 := newUnitTestLState()
	recv2 := newUnitTestLState()
	recv3 := newUnitTestLState()
	sender1 := newUnitTestLState()
	sender2 := newUnitTestLState()
	sender3 := newUnitTestLState()

	ch.Receive(recv1, nil)
	ch.Receive(recv2, nil)
	ch.Receive(recv3, nil)

	r1 := ch.Send(sender1, lua.LNumber(10), nil)
	var woken1 *lua.LState
	for _, u := range r1.GetUpdates() {
		if u.State != sender1 {
			woken1 = u.State
		}
	}
	if woken1 != recv1 {
		t.Error("first send should wake first receiver")
	}

	r2 := ch.Send(sender2, lua.LNumber(20), nil)
	var woken2 *lua.LState
	for _, u := range r2.GetUpdates() {
		if u.State != sender2 {
			woken2 = u.State
		}
	}
	if woken2 != recv2 {
		t.Error("second send should wake second receiver")
	}

	r3 := ch.Send(sender3, lua.LNumber(30), nil)
	var woken3 *lua.LState
	for _, u := range r3.GetUpdates() {
		if u.State != sender3 {
			woken3 = u.State
		}
	}
	if woken3 != recv3 {
		t.Error("third send should wake third receiver")
	}
}

// ============================================================================
// CLOSE SEMANTICS TESTS
// ============================================================================

func TestChannel_CloseSendError(t *testing.T) {
	ch := NewChannel(1)
	sender := newUnitTestLState()
	closer := newUnitTestLState()

	ch.Close(closer)
	r := ch.Send(sender, lua.LNumber(1), nil)
	updates := r.GetUpdates()
	if len(updates) != 1 {
		t.Fatal("expected 1 update")
	}
	if updates[0].Error == nil {
		t.Error("send on closed channel should return error")
	}
	if updates[0].Error.Error() != "send on closed channel" {
		t.Errorf("unexpected error: %v", updates[0].Error)
	}
}

func TestChannel_CloseBlockedSendersError(t *testing.T) {
	ch := NewChannel(0)
	sender1 := newUnitTestLState()
	sender2 := newUnitTestLState()
	closer := newUnitTestLState()

	ch.Send(sender1, lua.LNumber(1), nil)
	ch.Send(sender2, lua.LNumber(2), nil)

	closeResult := ch.Close(closer)
	if closeResult == nil {
		t.Fatal("close should return result with blocked senders")
	}

	senderErrors := 0
	for _, u := range closeResult.GetUpdates() {
		if u.State == sender1 || u.State == sender2 {
			if u.Error == nil {
				t.Error("blocked sender should get error on close")
			}
			senderErrors++
		}
	}
	if senderErrors != 2 {
		t.Errorf("expected 2 sender errors, got %d", senderErrors)
	}
}

func TestChannel_CloseBlockedReceiversNilFalse(t *testing.T) {
	ch := NewChannel(0)
	recv1 := newUnitTestLState()
	recv2 := newUnitTestLState()
	closer := newUnitTestLState()

	ch.Receive(recv1, nil)
	ch.Receive(recv2, nil)

	closeResult := ch.Close(closer)
	if closeResult == nil {
		t.Fatal("close should return result with blocked receivers")
	}

	receiverWakes := 0
	for _, u := range closeResult.GetUpdates() {
		if u.State == recv1 || u.State == recv2 {
			if u.Error != nil {
				t.Error("blocked receiver should not get error on close")
			}
			result := u.GetResult()
			if len(result) < 2 {
				t.Error("receiver should get 2 values")
				continue
			}
			if result[0] != lua.LNil {
				t.Errorf("receiver should get nil, got %v", result[0])
			}
			if result[1] != lua.LFalse {
				t.Errorf("receiver should get ok=false, got %v", result[1])
			}
			receiverWakes++
		}
	}
	if receiverWakes != 2 {
		t.Errorf("expected 2 receiver wakes, got %d", receiverWakes)
	}
}

func TestChannel_ClosePreservesBufferedValues(t *testing.T) {
	ch := NewChannel(3)
	sender := newUnitTestLState()
	recv := newUnitTestLState()
	closer := newUnitTestLState()

	ch.Send(sender, lua.LNumber(1), nil)
	ch.Send(sender, lua.LNumber(2), nil)
	ch.Send(sender, lua.LNumber(3), nil)
	ch.Close(closer)

	for i := 1; i <= 3; i++ {
		r := ch.Receive(recv, nil)
		updates := r.GetUpdates()
		if len(updates) != 1 {
			t.Fatalf("receive %d: expected 1 update", i)
		}
		result := updates[0].GetResult()
		if result[0] != lua.LNumber(float64(i)) {
			t.Errorf("receive %d: expected %d, got %v", i, i, result[0])
		}
		if result[1] != lua.LTrue {
			t.Errorf("receive %d: expected ok=true", i)
		}
	}

	r4 := ch.Receive(recv, nil)
	result4 := r4.GetUpdates()[0].GetResult()
	if result4[0] != lua.LNil {
		t.Errorf("expected nil after drain, got %v", result4[0])
	}
	if result4[1] != lua.LFalse {
		t.Errorf("expected ok=false after drain, got %v", result4[1])
	}
}

func TestChannel_DoubleCloseError(t *testing.T) {
	ch := NewChannel(0)
	closer := newUnitTestLState()

	ch.Close(closer)
	r := ch.Close(closer)
	if r == nil || len(r.Updates) == 0 || r.Updates[0].Error == nil {
		t.Error("double close should return error result")
	}
}

// ============================================================================
// MIXED BUFFERED + BLOCKED SENDERS
// ============================================================================

func TestChannel_MixedBufferedAndBlockedSenders(t *testing.T) {
	ch := NewChannel(2)
	sender1 := newUnitTestLState()
	sender2 := newUnitTestLState()
	blocker := newUnitTestLState()
	recv := newUnitTestLState()

	ch.Send(sender1, lua.LNumber(1), nil)
	ch.Send(sender2, lua.LNumber(2), nil)

	blockResult := ch.Send(blocker, lua.LNumber(3), nil)
	if !blockResult.Yields {
		t.Fatal("third sender should block")
	}

	r1 := ch.Receive(recv, nil)
	var val1 lua.LValue
	var blockerWoken bool
	for _, u := range r1.GetUpdates() {
		if u.State == recv {
			val1 = u.GetResult()[0]
		}
		if u.State == blocker && u.Error == nil {
			blockerWoken = true
		}
	}
	if val1 != lua.LNumber(1) {
		t.Errorf("first receive expected 1, got %v", val1)
	}
	if !blockerWoken {
		t.Error("blocked sender should wake when buffer space frees")
	}

	r2 := ch.Receive(recv, nil)
	var val2 lua.LValue
	for _, u := range r2.GetUpdates() {
		if u.State == recv {
			val2 = u.GetResult()[0]
		}
	}
	if val2 != lua.LNumber(2) {
		t.Errorf("second receive expected 2, got %v", val2)
	}

	r3 := ch.Receive(recv, nil)
	var val3 lua.LValue
	for _, u := range r3.GetUpdates() {
		if u.State == recv {
			val3 = u.GetResult()[0]
		}
	}
	if val3 != lua.LNumber(3) {
		t.Errorf("third receive expected 3, got %v", val3)
	}
}

func TestChannel_MultipleBlockedSendersWithBuffer(t *testing.T) {
	ch := NewChannel(1)
	sender := newUnitTestLState()
	blocker1 := newUnitTestLState()
	blocker2 := newUnitTestLState()
	blocker3 := newUnitTestLState()
	recv := newUnitTestLState()

	ch.Send(sender, lua.LNumber(1), nil)
	ch.Send(blocker1, lua.LNumber(2), nil)
	ch.Send(blocker2, lua.LNumber(3), nil)
	ch.Send(blocker3, lua.LNumber(4), nil)

	r1 := ch.Receive(recv, nil)
	var val1 lua.LValue
	var woken1 bool
	for _, u := range r1.GetUpdates() {
		if u.State == recv {
			val1 = u.GetResult()[0]
		}
		if u.State == blocker1 && u.Error == nil {
			woken1 = true
		}
	}
	if val1 != lua.LNumber(1) {
		t.Errorf("expected 1, got %v", val1)
	}
	if !woken1 {
		t.Error("blocker1 should wake when buffer space frees")
	}

	r2 := ch.Receive(recv, nil)
	var val2 lua.LValue
	var woken2 bool
	for _, u := range r2.GetUpdates() {
		if u.State == recv {
			val2 = u.GetResult()[0]
		}
		if u.State == blocker2 && u.Error == nil {
			woken2 = true
		}
	}
	if val2 != lua.LNumber(2) {
		t.Errorf("expected 2, got %v", val2)
	}
	if !woken2 {
		t.Error("blocker2 should wake when buffer space frees")
	}

	r3 := ch.Receive(recv, nil)
	var val3 lua.LValue
	var woken3 bool
	for _, u := range r3.GetUpdates() {
		if u.State == recv {
			val3 = u.GetResult()[0]
		}
		if u.State == blocker3 && u.Error == nil {
			woken3 = true
		}
	}
	if val3 != lua.LNumber(3) {
		t.Errorf("expected 3, got %v", val3)
	}
	if !woken3 {
		t.Error("blocker3 should wake when buffer space frees")
	}

	r4 := ch.Receive(recv, nil)
	var val4 lua.LValue
	for _, u := range r4.GetUpdates() {
		if u.State == recv {
			val4 = u.GetResult()[0]
		}
	}
	if val4 != lua.LNumber(4) {
		t.Errorf("expected 4, got %v", val4)
	}
}

// ============================================================================
// SIZE TRACKING TESTS
// ============================================================================

func TestChannel_SizeTracking(t *testing.T) {
	ch := NewChannel(3)
	sender := newUnitTestLState()
	recv := newUnitTestLState()

	if ch.Size() != 0 {
		t.Errorf("initial size should be 0, got %d", ch.Size())
	}

	ch.Send(sender, lua.LNumber(1), nil)
	if ch.Size() != 1 {
		t.Errorf("after 1 send, size should be 1, got %d", ch.Size())
	}

	ch.Send(sender, lua.LNumber(2), nil)
	if ch.Size() != 2 {
		t.Errorf("after 2 sends, size should be 2, got %d", ch.Size())
	}

	ch.Receive(recv, nil)
	if ch.Size() != 1 {
		t.Errorf("after receive, size should be 1, got %d", ch.Size())
	}
}

// ============================================================================
// CANSEND/CANRECEIVE PREDICATE TESTS
// ============================================================================

func TestChannel_CanSendBuffered(t *testing.T) {
	ch := NewChannel(1)
	sender := newUnitTestLState()

	if !ch.CanSend() {
		t.Error("should be able to send to empty buffer")
	}

	ch.Send(sender, lua.LNumber(1), nil)

	if ch.CanSend() {
		t.Error("should not be able to send to full buffer without receiver")
	}
}

func TestChannel_CanSendUnbufferedWithReceiver(t *testing.T) {
	ch := NewChannel(0)
	recv := newUnitTestLState()

	if ch.CanSend() {
		t.Error("unbuffered channel without receiver should not allow send")
	}

	ch.Receive(recv, nil)

	if !ch.CanSend() {
		t.Error("unbuffered channel with waiting receiver should allow send")
	}
}

func TestChannel_CanReceive(t *testing.T) {
	ch := NewChannel(1)
	sender := newUnitTestLState()

	if ch.CanReceive() {
		t.Error("empty channel should not allow receive")
	}

	ch.Send(sender, lua.LNumber(1), nil)

	if !ch.CanReceive() {
		t.Error("non-empty channel should allow receive")
	}
}

func TestChannel_CanReceiveClosed(t *testing.T) {
	ch := NewChannel(0)
	closer := newUnitTestLState()

	if ch.CanReceive() {
		t.Error("new empty channel should not allow receive")
	}

	ch.Close(closer)

	if !ch.CanReceive() {
		t.Error("closed channel should allow receive")
	}
}

func TestChannel_CanReceiveClosedBufferedDrain(t *testing.T) {
	ch := NewChannel(1)
	sender := newUnitTestLState()
	receiver := newUnitTestLState()

	ch.Send(sender, lua.LString("buffered"), nil)
	ch.Close(sender)

	if !ch.CanReceive() {
		t.Error("closed buffered channel should allow receive before draining buffered value")
	}

	ch.Receive(receiver, nil)

	if !ch.CanReceive() {
		t.Error("closed drained channel should still allow receive for ok=false")
	}
}

func TestChannel_SlotsCount(t *testing.T) {
	ch := NewChannel(3)
	sender := newUnitTestLState()
	recv := newUnitTestLState()

	if ch.Slots() != 3 {
		t.Errorf("initial slots should be 3, got %d", ch.Slots())
	}

	ch.Send(sender, lua.LNumber(1), nil)
	if ch.Slots() != 2 {
		t.Errorf("after 1 send, slots should be 2, got %d", ch.Slots())
	}

	ch.Receive(recv, nil)
	if ch.Slots() != 3 {
		t.Errorf("after receive, slots should be 3, got %d", ch.Slots())
	}
}

// ============================================================================
// CHANNEL IDENTITY TESTS
// ============================================================================

func TestChannel_PushIdempotent(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	LoadModuleDef(l, ChannelModule)

	ch := NewChannel(10)

	ud1 := PushChannel(l, ch)
	l.Pop(1)

	ud2 := PushChannel(l, ch)
	l.Pop(1)

	ud3 := PushChannel(l, ch)
	l.Pop(1)

	if ud1 != ud2 {
		t.Error("PushChannel should return same userdata (call 1 vs 2)")
	}
	if ud2 != ud3 {
		t.Error("PushChannel should return same userdata (call 2 vs 3)")
	}
	if ch.Value() != ud1 {
		t.Error("channel should cache the userdata")
	}
}

func TestChannel_ValueCaching(t *testing.T) {
	ch := NewChannel(10)

	if ch.Value() != nil {
		t.Error("new channel should have nil value")
	}

	l := lua.NewState()
	defer l.Close()
	LoadModuleDef(l, ChannelModule)

	ud := PushChannel(l, ch)

	if ch.Value() == nil {
		t.Error("channel value should be set after PushChannel")
	}
	if ch.Value() != ud {
		t.Error("channel value should match the userdata returned by PushChannel")
	}

	ud2 := PushChannel(l, ch)
	if ud != ud2 {
		t.Error("PushChannel should return same userdata for same channel")
	}
}

// ============================================================================
// SUBSCRIBE CONTEXT TESTS
// ============================================================================

func TestSubscribeContext_CreatesChannel(t *testing.T) {
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
	if _, ok := ctx.byTopic["topic1"]; !ok {
		t.Error("topic should be in byTopic map")
	}
	if _, ok := ctx.byChannel[sub.channel]; !ok {
		t.Error("channel should be in byChannel map")
	}
}

func TestSubscribeContext_ReturnsSameChannel(t *testing.T) {
	ctx := &subscribeContext{
		byTopic:   make(map[string]*subscription),
		byChannel: make(map[*Channel]string),
	}

	sub1, err := ctx.add("topic1", 10)
	if err != nil {
		t.Fatalf("first add failed: %v", err)
	}

	sub2, err := ctx.add("topic1", 5)
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

func TestSubscribeContext_AddExisting(t *testing.T) {
	ctx := &subscribeContext{
		byTopic:   make(map[string]*subscription),
		byChannel: make(map[*Channel]string),
	}

	ch := NewChannel(10)
	sub, err := ctx.addExisting("topic1", ch, false)
	if err != nil {
		t.Fatalf("addExisting failed: %v", err)
	}

	if sub.channel != ch {
		t.Error("subscription should reference the provided channel")
	}

	sub2, err := ctx.addExisting("topic1", ch, false)
	if err != nil {
		t.Fatalf("second addExisting failed: %v", err)
	}
	if sub != sub2 {
		t.Error("should return same subscription")
	}

	ch2 := NewChannel(10)
	_, err = ctx.addExisting("topic1", ch2, false)
	if err == nil {
		t.Error("addExisting with different channel should fail")
	}
}

func TestSubscribeContext_SystemTopicIdentity(t *testing.T) {
	ctx := &subscribeContext{
		byTopic:   make(map[string]*subscription),
		byChannel: make(map[*Channel]string),
	}

	inboxSub, err := ctx.add(topology.TopicInbox, 0)
	if err != nil {
		t.Fatalf("inbox subscribe failed: %v", err)
	}

	eventsSub, err := ctx.add(topology.TopicEvents, 0)
	if err != nil {
		t.Fatalf("events subscribe failed: %v", err)
	}

	if inboxSub.channel == eventsSub.channel {
		t.Error("inbox and events should have different channels")
	}

	inboxSub2, _ := ctx.add(topology.TopicInbox, 0)
	if inboxSub.channel != inboxSub2.channel {
		t.Error("inbox channel identity not preserved")
	}
}

// ============================================================================
// LUA CHANNEL IDENTITY TESTS
// ============================================================================

func TestChannel_LuaComparison(t *testing.T) {
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

	proc := startChannelUnitProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 20); err != nil {
		t.Fatal(err)
	}
}

func TestChannel_SubscribeExistingReturnsIt(t *testing.T) {
	script := `
		local ch = channel.new(10)
		local result = subscribe("my_topic", ch)

		if result == nil then
			error("subscribe should return channel")
		end

		if result ~= ch then
			error("subscribe should return the same channel we provided")
		end

		return "ok"
	`

	proc := startChannelUnitProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 30); err != nil {
		t.Fatal(err)
	}
}

func TestChannel_SubscribeSameTopicDifferentChannelErrors(t *testing.T) {
	script := `
		local ch1 = channel.new(10)
		local ch2 = channel.new(10)

		local result1 = subscribe("test_topic", ch1)
		if result1 == nil then
			error("first subscribe should succeed")
		end

		local result2 = subscribe("test_topic", ch2)
		if result2 ~= nil then
			error("second subscribe with different channel should fail")
		end

		return "ok"
	`

	proc := startChannelUnitProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 30); err != nil {
		t.Fatal(err)
	}
}

func TestChannel_SubscribeRequestFields(t *testing.T) {
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

func TestChannel_MultipleDifferentTopics(t *testing.T) {
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

	proc := startChannelUnitProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 30); err != nil {
		t.Fatal(err)
	}
}

func TestChannel_IdentityAcrossSubscribe(t *testing.T) {
	script := `
		local ch = channel.new(10)
		subscribe("test_select", ch)

		local stored = ch

		if ch ~= stored then
			error("channel identity lost after subscribe")
		end

		return "ok"
	`

	proc := startChannelUnitProcess(t, script)
	defer proc.Close()

	if err := runUntilDone(t, proc, 30); err != nil {
		t.Fatal(err)
	}
}
