package engine

import (
	"testing"

	lua "github.com/yuin/gopher-lua"
)

// Channel Semantics Test Suite
//
// This test suite verifies Go channel semantics compliance for the channel implementation.
// These tests operate directly on Channel struct methods without involving Lua coroutines.
//
// KEY SEMANTICS TESTED:
// 1. Unbuffered channels: send blocks until receiver ready
// 2. Buffered channels: send buffers until full, then blocks
// 3. FIFO ordering: blocked operations wake in order
// 4. Close semantics: blocked senders error, blocked receivers get (nil, false)
// 5. Mixed buffered + blocked scenario: critical edge case

func newTestLState() *lua.LState {
	return lua.NewState()
}

// ============================================================================
// UNBUFFERED CHANNEL TESTS
// ============================================================================

// TestUnbufferedSendBlocks verifies that send on unbuffered channel blocks.
func TestUnbufferedSendBlocks(t *testing.T) {
	ch := NewChannel(0)
	sender := newTestLState()

	result := ch.Send(sender, lua.LString("value"), nil)
	if !result.Yields {
		t.Error("send on unbuffered channel without receiver should yield/block")
	}
	if len(result.Block) != 1 || result.Block[0] != ch {
		t.Error("blocked send should report blocking on this channel")
	}
}

// TestUnbufferedReceiveBlocks verifies that receive on empty unbuffered channel blocks.
func TestUnbufferedReceiveBlocks(t *testing.T) {
	ch := NewChannel(0)
	recv := newTestLState()

	result := ch.Receive(recv, nil)
	if !result.Yields {
		t.Error("receive on empty unbuffered channel should yield/block")
	}
	if len(result.Block) != 1 || result.Block[0] != ch {
		t.Error("blocked receive should report blocking on this channel")
	}
}

// TestUnbufferedDirectHandoff verifies direct handoff between sender and receiver.
func TestUnbufferedDirectHandoff(t *testing.T) {
	ch := NewChannel(0)
	recv := newTestLState()
	sender := newTestLState()

	// Receiver blocks first
	ch.Receive(recv, nil)

	// Sender should complete and wake receiver
	sendResult := ch.Send(sender, lua.LString("hello"), nil)
	if !sendResult.Yields {
		t.Error("send that synchronizes with receiver should yield")
	}

	updates := sendResult.GetUpdates()
	if len(updates) != 2 {
		t.Fatalf("expected 2 updates (sender + receiver), got %d", len(updates))
	}

	// Find updates
	var receiverUpdate, senderUpdate *TaskUpdate
	for _, u := range updates {
		if u.State == recv {
			receiverUpdate = u
		} else if u.State == sender {
			senderUpdate = u
		}
	}

	if receiverUpdate == nil {
		t.Fatal("missing receiver update")
	}
	if senderUpdate == nil {
		t.Fatal("missing sender update")
	}

	// Receiver should get (value, true)
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

// TestUnbufferedSenderWaitsForReceiver verifies send-first then receive works.
func TestUnbufferedSenderWaitsForReceiver(t *testing.T) {
	ch := NewChannel(0)
	recv := newTestLState()
	sender := newTestLState()

	// Sender blocks first
	ch.Send(sender, lua.LString("world"), nil)

	// Receiver should complete and wake sender
	recvResult := ch.Receive(recv, nil)
	if !recvResult.Yields {
		t.Error("receive that synchronizes with blocked sender should yield")
	}

	updates := recvResult.GetUpdates()
	if len(updates) != 2 {
		t.Fatalf("expected 2 updates (sender + receiver), got %d", len(updates))
	}

	// Verify receiver got the value
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

// TestBufferedSendDoesNotBlock verifies buffered send doesn't block when space available.
func TestBufferedSendDoesNotBlock(t *testing.T) {
	ch := NewChannel(2)
	sender := newTestLState()

	// First send
	r1 := ch.Send(sender, lua.LNumber(1), nil)
	if r1.Yields {
		t.Error("first buffered send should not block")
	}

	// Second send
	r2 := ch.Send(sender, lua.LNumber(2), nil)
	if r2.Yields {
		t.Error("second buffered send should not block")
	}
}

// TestBufferedSendBlocksWhenFull verifies buffered send blocks when buffer full.
func TestBufferedSendBlocksWhenFull(t *testing.T) {
	ch := NewChannel(2)
	sender := newTestLState()
	blocker := newTestLState()

	// Fill buffer
	ch.Send(sender, lua.LNumber(1), nil)
	ch.Send(sender, lua.LNumber(2), nil)

	// Third send should block
	r3 := ch.Send(blocker, lua.LNumber(3), nil)
	if !r3.Yields {
		t.Error("send to full buffer should block")
	}
	if len(r3.Block) != 1 || r3.Block[0] != ch {
		t.Error("blocked send should report blocking on channel")
	}
}

// TestBufferedReceiveDoesNotBlock verifies buffered receive doesn't block when data available.
func TestBufferedReceiveDoesNotBlock(t *testing.T) {
	ch := NewChannel(2)
	sender := newTestLState()
	recv := newTestLState()

	// Buffer a value
	ch.Send(sender, lua.LNumber(42), nil)

	// Receive should not block
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

// TestBufferedReceiveBlocksWhenEmpty verifies buffered receive blocks when empty.
func TestBufferedReceiveBlocksWhenEmpty(t *testing.T) {
	ch := NewChannel(2)
	recv := newTestLState()

	r := ch.Receive(recv, nil)
	if !r.Yields {
		t.Error("receive from empty buffer should block")
	}
}

// ============================================================================
// FIFO ORDERING TESTS
// ============================================================================

// TestFIFOSenderOrder verifies blocked senders wake in FIFO order.
func TestFIFOSenderOrder(t *testing.T) {
	ch := NewChannel(0) // unbuffered
	sender1 := newTestLState()
	sender2 := newTestLState()
	sender3 := newTestLState()
	recv1 := newTestLState()
	recv2 := newTestLState()
	recv3 := newTestLState()

	// Senders block in order
	ch.Send(sender1, lua.LNumber(1), nil)
	ch.Send(sender2, lua.LNumber(2), nil)
	ch.Send(sender3, lua.LNumber(3), nil)

	// First receive should get value 1
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

	// Second receive should get value 2
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

	// Third receive should get value 3
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

// TestFIFOReceiverOrder verifies blocked receivers wake in FIFO order.
func TestFIFOReceiverOrder(t *testing.T) {
	ch := NewChannel(0) // unbuffered
	recv1 := newTestLState()
	recv2 := newTestLState()
	recv3 := newTestLState()
	sender1 := newTestLState()
	sender2 := newTestLState()
	sender3 := newTestLState()

	// Receivers block in order
	ch.Receive(recv1, nil)
	ch.Receive(recv2, nil)
	ch.Receive(recv3, nil)

	// First send should wake recv1
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

	// Second send should wake recv2
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

	// Third send should wake recv3
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

// TestCloseSendError verifies send on closed channel returns error.
func TestCloseSendError(t *testing.T) {
	ch := NewChannel(1)
	sender := newTestLState()
	closer := newTestLState()

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

// TestCloseBlockedSendersError verifies blocked senders get error on close.
func TestCloseBlockedSendersError(t *testing.T) {
	ch := NewChannel(0)
	sender1 := newTestLState()
	sender2 := newTestLState()
	closer := newTestLState()

	// Block two senders
	ch.Send(sender1, lua.LNumber(1), nil)
	ch.Send(sender2, lua.LNumber(2), nil)

	// Close
	closeResult := ch.Close(closer)
	if closeResult == nil {
		t.Fatal("close should return result with blocked senders")
	}

	// Both senders should get errors
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

// TestCloseBlockedReceiversNilFalse verifies blocked receivers get (nil, false) on close.
func TestCloseBlockedReceiversNilFalse(t *testing.T) {
	ch := NewChannel(0)
	recv1 := newTestLState()
	recv2 := newTestLState()
	closer := newTestLState()

	// Block two receivers
	ch.Receive(recv1, nil)
	ch.Receive(recv2, nil)

	// Close
	closeResult := ch.Close(closer)
	if closeResult == nil {
		t.Fatal("close should return result with blocked receivers")
	}

	// Both receivers should get (nil, false)
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

// TestClosePreservesBufferedValues verifies buffered values can be drained after close.
func TestClosePreservesBufferedValues(t *testing.T) {
	ch := NewChannel(3)
	sender := newTestLState()
	recv := newTestLState()
	closer := newTestLState()

	// Buffer 3 values
	ch.Send(sender, lua.LNumber(1), nil)
	ch.Send(sender, lua.LNumber(2), nil)
	ch.Send(sender, lua.LNumber(3), nil)

	// Close
	ch.Close(closer)

	// Should receive all buffered values with ok=true
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

	// Fourth receive should get (nil, false)
	r4 := ch.Receive(recv, nil)
	result4 := r4.GetUpdates()[0].GetResult()
	if result4[0] != lua.LNil {
		t.Errorf("expected nil after drain, got %v", result4[0])
	}
	if result4[1] != lua.LFalse {
		t.Errorf("expected ok=false after drain, got %v", result4[1])
	}
}

// TestDoubleCloseNil verifies double close returns nil.
func TestDoubleCloseNil(t *testing.T) {
	ch := NewChannel(0)
	closer := newTestLState()

	ch.Close(closer)
	r := ch.Close(closer)
	if r != nil {
		t.Error("double close should return nil")
	}
}

// ============================================================================
// CRITICAL EDGE CASE: MIXED BUFFERED + BLOCKED SENDERS
// ============================================================================

// TestMixedBufferedAndBlockedSenders tests the scenario where:
// 1. Buffer has values
// 2. Blocked senders are waiting (buffer was full when they sent)
// 3. Receive drains FIFO: buffered values first, then blocked sender values
// Go semantics: blocked sender wakes when buffer space frees, their value moves to buffer
func TestMixedBufferedAndBlockedSenders(t *testing.T) {
	ch := NewChannel(2) // capacity 2
	sender1 := newTestLState()
	sender2 := newTestLState()
	blocker := newTestLState()
	recv := newTestLState()

	// Fill buffer: queue = [buf(1), buf(2)]
	ch.Send(sender1, lua.LNumber(1), nil)
	ch.Send(sender2, lua.LNumber(2), nil)

	// Third sender blocks: queue = [buf(1), buf(2), blocked(3)]
	blockResult := ch.Send(blocker, lua.LNumber(3), nil)
	if !blockResult.Yields {
		t.Fatal("third sender should block")
	}

	// First receive: pop buf(1), blocked(3) wakes and becomes buf(3)
	// queue becomes = [buf(2), buf(3)]
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
		t.Error("blocked sender should wake when buffer space frees (Go semantics)")
	}

	// Second receive: pop buf(2), queue = [buf(3)]
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

	// Third receive: pop buf(3), queue = []
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

// TestMultipleBlockedSendersWithBuffer tests multiple blocked senders wake in FIFO order.
// Go semantics: blocked senders wake when buffer space frees, values received in FIFO order.
func TestMultipleBlockedSendersWithBuffer(t *testing.T) {
	ch := NewChannel(1) // capacity 1
	sender := newTestLState()
	blocker1 := newTestLState()
	blocker2 := newTestLState()
	blocker3 := newTestLState()
	recv := newTestLState()

	// Buffer one value: queue = [buf(1)]
	ch.Send(sender, lua.LNumber(1), nil)

	// Three senders block: queue = [buf(1), blocked(2), blocked(3), blocked(4)]
	ch.Send(blocker1, lua.LNumber(2), nil)
	ch.Send(blocker2, lua.LNumber(3), nil)
	ch.Send(blocker3, lua.LNumber(4), nil)

	// First receive: get 1, blocker1 wakes (2 becomes buffered)
	// queue = [buf(2), blocked(3), blocked(4)]
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

	// Second receive: get 2, blocker2 wakes (3 becomes buffered)
	// queue = [buf(3), blocked(4)]
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

	// Third receive: get 3, blocker3 wakes (4 becomes buffered)
	// queue = [buf(4)]
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

	// Fourth receive: get 4, no more blocked senders
	// queue = []
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

// TestSizeTrackingBuffered verifies size tracks buffered values correctly.
func TestSizeTrackingBuffered(t *testing.T) {
	ch := NewChannel(3)
	sender := newTestLState()
	recv := newTestLState()

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
// CANSENDSEND/CANRECEIVE PREDICATE TESTS
// ============================================================================

// TestCanSendBuffered verifies CanSend for buffered channel.
func TestCanSendBuffered(t *testing.T) {
	ch := NewChannel(1)
	sender := newTestLState()

	// Empty buffer: can send
	if !ch.CanSend() {
		t.Error("should be able to send to empty buffer")
	}

	// Fill buffer
	ch.Send(sender, lua.LNumber(1), nil)

	// Full buffer, no receiver: cannot send
	if ch.CanSend() {
		t.Error("should not be able to send to full buffer without receiver")
	}
}

// TestCanSendUnbufferedWithReceiver verifies CanSend with waiting receiver.
func TestCanSendUnbufferedWithReceiver(t *testing.T) {
	ch := NewChannel(0) // unbuffered
	recv := newTestLState()

	// No receiver: cannot send
	if ch.CanSend() {
		t.Error("unbuffered channel without receiver should not allow send")
	}

	// Block receiver
	ch.Receive(recv, nil)

	// With waiting receiver: can send
	if !ch.CanSend() {
		t.Error("unbuffered channel with waiting receiver should allow send")
	}
}

// TestCanReceive verifies CanReceive predicate.
func TestCanReceive(t *testing.T) {
	ch := NewChannel(1)
	sender := newTestLState()

	// Empty: cannot receive
	if ch.CanReceive() {
		t.Error("empty channel should not allow receive")
	}

	// Buffer a value
	ch.Send(sender, lua.LNumber(1), nil)

	// Non-empty: can receive
	if !ch.CanReceive() {
		t.Error("non-empty channel should allow receive")
	}
}

// TestSlotsCount verifies Slots returns correct available slots.
func TestSlotsCount(t *testing.T) {
	ch := NewChannel(3)
	sender := newTestLState()
	recv := newTestLState()

	// Initially: 3 slots (full capacity)
	if ch.Slots() != 3 {
		t.Errorf("initial slots should be 3, got %d", ch.Slots())
	}

	// After 1 send: 2 slots
	ch.Send(sender, lua.LNumber(1), nil)
	if ch.Slots() != 2 {
		t.Errorf("after 1 send, slots should be 2, got %d", ch.Slots())
	}

	// Block a receiver: adds 1 to slots
	ch.Receive(recv, nil) // consumes buffered value
	// Now buffer is empty again
	if ch.Slots() != 3 {
		t.Errorf("after receive, slots should be 3, got %d", ch.Slots())
	}
}
