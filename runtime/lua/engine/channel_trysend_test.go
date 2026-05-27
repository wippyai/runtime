// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"testing"

	lua "github.com/wippyai/go-lua"
)

// Channel.TrySend is the nonblocking send primitive used by external
// producers that route through deliverMessage / the ephemeral router. It
// must:
//
//   - Hand off directly to a waiting receiver.
//   - Push into the buffer if there is room.
//   - Return a "not sent" result when the channel is full and no receiver
//     is waiting, WITHOUT pushing a phantom blocked-sender into sendq.
//   - Return an error result when the channel is closed.

// TestTrySend_HandoffToWaitingReceiver: with a blocked receiver waiting,
// TrySend hands the value directly and the receiver wakes.
func TestTrySend_HandoffToWaitingReceiver(t *testing.T) {
	ch := NewChannel(0)
	state := lua.NewState()
	defer state.Close()

	// Block a receiver.
	recvResult := ch.Receive(state, nil)
	if recvResult == nil || !recvResult.Yields {
		t.Fatalf("expected receive to block on unbuffered channel, got result=%v", recvResult)
	}
	ReleaseResult(recvResult)

	if ch.recvq.Len() != 1 {
		t.Fatalf("precondition: expected 1 blocked receiver, got %d", ch.recvq.Len())
	}

	result, sent := ch.TrySend(lua.LString("hello"))
	if !sent {
		t.Fatal("TrySend should report sent when a receiver is waiting")
	}
	if result == nil {
		t.Fatal("expected ChannelResult from handoff")
	}
	if len(result.Updates) != 1 {
		t.Fatalf("expected exactly one update (the woken receiver), got %d", len(result.Updates))
	}
	ReleaseResult(result)

	if ch.recvq.Len() != 0 {
		t.Fatalf("receiver should have been removed from recvq, len=%d", ch.recvq.Len())
	}
	if ch.sendq.Len() != 0 {
		t.Fatalf("sendq should remain empty, len=%d", ch.sendq.Len())
	}
}

// TestTrySend_PushesToBufferWithRoom: cap-1 channel, no receiver waiting,
// TrySend buffers the value.
func TestTrySend_PushesToBufferWithRoom(t *testing.T) {
	ch := NewChannel(1)

	result, sent := ch.TrySend(lua.LString("x"))
	if !sent {
		t.Fatal("TrySend should report sent when buffer has room")
	}
	if result != nil {
		// A buffered TrySend with no other party to wake should return nil
		// to keep the hot path allocation-free. If a result IS returned it
		// must be empty.
		if len(result.Updates) != 0 || len(result.Block) != 0 {
			t.Errorf("buffered TrySend should not produce task updates or blocks, got %+v", result)
		}
		ReleaseResult(result)
	}

	if ch.buffer.Len() != 1 {
		t.Fatalf("buffer should hold the value, len=%d", ch.buffer.Len())
	}
	if ch.sendq.Len() != 0 {
		t.Fatalf("sendq should remain empty, len=%d", ch.sendq.Len())
	}
}

// TestTrySend_ReturnsNotSentOnFull: cap-1 channel filled, no receiver,
// TrySend returns sent=false and never creates a phantom blocked-sender.
func TestTrySend_ReturnsNotSentOnFull(t *testing.T) {
	ch := NewChannel(1)
	ch.buffer.PushBack(lua.LString("seed")) // simulate full buffer

	result, sent := ch.TrySend(lua.LString("overflow"))
	if sent {
		t.Fatal("TrySend on full channel should report sent=false")
	}
	if result != nil {
		// allowed to be nil; if not nil, must be empty
		if len(result.Updates) != 0 || len(result.Block) != 0 {
			t.Errorf("not-sent TrySend should not produce updates or blocks: result=%+v", result)
		}
		ReleaseResult(result)
	}

	if ch.sendq.Len() != 0 {
		t.Fatalf("TrySend must NOT push a phantom blocked-sender on full channel, sendq.Len=%d", ch.sendq.Len())
	}
	if ch.buffer.Len() != 1 {
		t.Fatalf("buffer should be unchanged at len=1, got %d", ch.buffer.Len())
	}
}

// TestTrySend_OnClosedChannel: TrySend on a closed channel returns an error
// result and does not touch buffer/sendq.
func TestTrySend_OnClosedChannel(t *testing.T) {
	ch := NewChannel(1)
	closeResult := ch.Close(nil)
	if closeResult != nil {
		ReleaseResult(closeResult)
	}

	if !ch.IsClosed() {
		t.Fatal("precondition: channel should be closed")
	}

	result, sent := ch.TrySend(lua.LString("after-close"))
	if sent {
		t.Fatal("TrySend on closed channel must report sent=false")
	}
	if result == nil {
		t.Fatal("TrySend on closed channel must return an error result")
	}
	if len(result.Updates) != 1 || result.Updates[0].Error == nil {
		t.Errorf("expected one error update, got %+v", result.Updates)
	}
	ReleaseResult(result)

	if ch.buffer.Len() != 0 {
		t.Fatalf("closed channel buffer should be empty, got %d", ch.buffer.Len())
	}
	if ch.sendq.Len() != 0 {
		t.Fatalf("closed channel sendq should remain empty, got %d", ch.sendq.Len())
	}
}

// TestTrySend_RepeatedOverflowDoesNotGrowSendq: many TrySend calls into a
// full buffer must not accumulate phantom entries.
func TestTrySend_RepeatedOverflowDoesNotGrowSendq(t *testing.T) {
	ch := NewChannel(1)
	ch.buffer.PushBack(lua.LString("seed"))

	const N = 1000
	for i := 0; i < N; i++ {
		result, sent := ch.TrySend(lua.LString("x"))
		if sent {
			t.Fatalf("iteration %d: TrySend should have reported sent=false", i)
		}
		if result != nil {
			ReleaseResult(result)
		}
	}

	if ch.sendq.Len() != 0 {
		t.Fatalf("sendq grew under overflow: got %d, want 0", ch.sendq.Len())
	}
	if ch.buffer.Len() != 1 {
		t.Fatalf("buffer should remain at 1, got %d", ch.buffer.Len())
	}
}
