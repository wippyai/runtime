package channel

import (
	lua "github.com/yuin/gopher-lua"
	"testing"
)

func TestChannelBasicOperations(t *testing.T) {
	t.Run("unbuffered send/receive", func(t *testing.T) {
		ch := newChannel(0)
		L1 := lua.NewState()
		L2 := lua.NewState()

		value := lua.LString("test")
		next := ch.send(L1, value, nil)

		if !next.yields {
			t.Error("sender should yield on unbuffered channel")
		}
		if len(next.next) != 0 {
			t.Error("unexpected next for blocked send")
		}

		next = ch.receive(L2, nil)
		if !next.yields {
			t.Error("expected yields=true for completing send/receive")
		}
		if len(next.next) != 2 {
			t.Error("expected 2 next for send/receive pair")
		}

		if next.next[1].values[0] != value {
			t.Error("received wrong value")
		}
	})

	t.Run("buffered operations", func(t *testing.T) {
		ch := newChannel(1)
		L := lua.NewState()

		value := lua.LString("test")
		next := ch.send(L, value, nil)

		if next.yields {
			t.Error("send on non-full buffered channel shouldn't yield")
		}

		if !ch.isFull() {
			t.Error("channel should be full after send")
		}

		next = ch.receive(L, nil)
		if len(next.next) != 1 {
			t.Error("expected 1 result for buffered receive")
		}

		if next.next[0].values[0] != value {
			t.Error("received wrong value from buffer")
		}
	})

	t.Run("close channel", func(t *testing.T) {
		ch := newChannel(0)
		L := lua.NewState()

		next := ch.close(L)
		if next.yields {
			t.Error("close on empty channel shouldn't yield")
		}

		next = ch.send(L, lua.LString("test"), nil)
		if next.next[0].err == nil {
			t.Error("expected error on send to closed channel")
		}

		next = ch.receive(L, nil)
		if next.next[0].values[1] != lua.LFalse {
			t.Error("receive on closed channel should return ok=false")
		}
	})
}

func TestSelectOperations(t *testing.T) {
	t.Run("select send", func(t *testing.T) {
		ch := newChannel(0)
		L := lua.NewState()

		selectOp := &selectOp{
			cases: []*op{{
				kind:     sendOp,
				ch:       ch,
				value:    lua.LString("test"),
				task:     L,
				selectOp: nil,
			}},
			task: L,
		}
		selectOp.cases[0].selectOp = selectOp

		next := ch.send(L, lua.LString("test"), selectOp)
		if !next.yields {
			t.Error("select send should yield when no receiver")
		}
	})

	t.Run("select receive", func(t *testing.T) {
		ch := newChannel(0)
		L := lua.NewState()

		selectOp := &selectOp{
			cases: []*op{{
				kind:     receiveOp,
				ch:       ch,
				task:     L,
				selectOp: nil,
			}},
			task: L,
		}
		selectOp.cases[0].selectOp = selectOp

		next := ch.receive(L, selectOp)
		if !next.yields {
			t.Error("select receive should yield when no sender")
		}
	})
}

func TestChannelEdgeCases(t *testing.T) {
	t.Run("close full buffered channel", func(t *testing.T) {
		ch := newChannel(1)
		L := lua.NewState()

		ch.send(L, lua.LString("test"), nil)
		next := ch.close(L)

		if next.yields {
			t.Error("close shouldn't yield with only buffered values")
		}

		next = ch.receive(L, nil)
		if next.next[0].values[1] != lua.LTrue {
			t.Error("should receive buffered value successfully")
		}

		next = ch.receive(L, nil)
		if next.next[0].values[1] != lua.LFalse {
			t.Error("second receive should indicate closed channel")
		}
	})

	t.Run("close channel with pending operations", func(t *testing.T) {
		ch := newChannel(0)
		L1 := lua.NewState()

		// Add a blocked sender
		ch.send(L1, lua.LString("test"), nil)

		// Close the channel
		next := ch.close(L1)
		if len(next.next) != 2 {
			t.Errorf("expected 2 next (sender error + closer), got %d", len(next.next))
		}

		senderResult := next.next[0]
		if senderResult.err == nil || senderResult.err.Error() != "send on closed channel" {
			t.Error("expected send on closed channel error")
		}
		if senderResult.task != L1 {
			t.Error("wrong task in sender result")
		}

		closerResult := next.next[1]
		if closerResult.task != L1 {
			t.Error("wrong task in closer result")
		}
		if closerResult.values != nil {
			t.Error("closer should have nil values")
		}
	})
}

func TestNamedChannels(t *testing.T) {
	t.Run("named channel creation", func(t *testing.T) {
		ch := Named("test", 1)
		if !ch.isNamed() {
			t.Error("channel should be named")
		}
		if ch.name != "test" {
			t.Error("wrong channel name")
		}
	})
}

func TestChannelWaits(t *testing.T) {
	t.Run("unbuffered send block", func(t *testing.T) {
		ch := newChannel(0)
		L := lua.NewState()

		next := ch.send(L, lua.LString("test"), nil)

		if !next.yields {
			t.Error("send should yield on unbuffered channel")
		}
		if len(next.block) != 1 {
			t.Error("expected exactly one wait channel")
		}
		if next.block[0] != ch {
			t.Error("wait should be on the sending channel")
		}
	})

	t.Run("unbuffered receive block", func(t *testing.T) {
		ch := newChannel(0)
		L := lua.NewState()

		next := ch.receive(L, nil)

		if !next.yields {
			t.Error("receive should yield on empty unbuffered channel")
		}
		if len(next.block) != 1 {
			t.Error("expected exactly one wait channel")
		}
		if next.block[0] != ch {
			t.Error("wait should be on the receiving channel")
		}
	})

	t.Run("buffered send no wait when not full", func(t *testing.T) {
		ch := newChannel(1)
		L := lua.NewState()

		next := ch.send(L, lua.LString("test"), nil)

		if next.yields {
			t.Error("send shouldn't yield on non-full buffered channel")
		}
		if len(next.block) != 0 {
			t.Error("should have no wait channels for non-blocking send")
		}
	})

	t.Run("buffered send block when full", func(t *testing.T) {
		ch := newChannel(1)
		L := lua.NewState()

		// Fill the buffer first
		ch.send(L, lua.LString("test1"), nil)

		// Try to send when buffer is full
		next := ch.send(L, lua.LString("test2"), nil)

		if !next.yields {
			t.Error("send should yield on full buffered channel")
		}
		if len(next.block) != 1 {
			t.Error("expected exactly one wait channel")
		}
		if next.block[0] != ch {
			t.Error("wait should be on the sending channel")
		}
	})

	t.Run("no block on completed operations", func(t *testing.T) {
		ch := newChannel(0)
		L1 := lua.NewState()
		L2 := lua.NewState()

		// Set up a receiver first
		ch.receive(L1, nil)

		// Send should complete immediately
		next := ch.send(L2, lua.LString("test"), nil)

		if len(next.block) != 0 {
			t.Error("completed operation should have no block")
		}
	})

	t.Run("no block on closed channel operations", func(t *testing.T) {
		ch := newChannel(0)
		L := lua.NewState()

		ch.close(L)

		// Try operations on closed channel
		sendNext := ch.send(L, lua.LString("test"), nil)
		receiveNext := ch.receive(L, nil)

		if len(sendNext.block) != 0 {
			t.Error("send on closed channel should have no block")
		}
		if len(receiveNext.block) != 0 {
			t.Error("receive on closed channel should have no block")
		}
	})
}

func TestChannelReleaseLogic(t *testing.T) {
	t.Run("direct operation release", func(t *testing.T) {
		ch := newChannel(0)
		L := lua.NewState()

		// Create a direct send operation
		op := &op{
			kind: sendOp,
			ch:   ch,
			task: L,
		}

		releases := release(op)
		if len(releases) != 1 {
			t.Error("direct operation should release exactly one channel")
		}
		if releases[0] != ch {
			t.Error("released channel should be the operation's channel")
		}
	})

	t.Run("select operation release", func(t *testing.T) {
		ch1 := newChannel(0)
		ch2 := newChannel(0)
		L := lua.NewState()

		// Create a select operation with two cases
		selectOp := &selectOp{
			task: L,
			cases: []*op{
				{
					kind: sendOp,
					ch:   ch1,
					task: L,
				},
				{
					kind: receiveOp,
					ch:   ch2,
					task: L,
				},
			},
		}

		// Set up the select operation references
		selectOp.cases[0].selectOp = selectOp
		selectOp.cases[1].selectOp = selectOp

		// Add operations to channels
		ch1.senders.PushBack(selectOp.cases[0])
		ch2.receivers.PushBack(selectOp.cases[1])
		ch1.size++

		releases := release(selectOp.cases[0])
		if len(releases) != 2 {
			t.Errorf("select operation should release all channels, got %d releases", len(releases))
		}

		// Verify channels are cleaned up
		if ch1.senders.Len() != 0 || ch2.receivers.Len() != 0 {
			t.Error("channels should have no pending operations after release")
		}
		if ch1.size != 0 {
			t.Error("channel size should be reset after release")
		}
	})

	t.Run("multiple select operations on same channel", func(t *testing.T) {
		ch := newChannel(0)
		L1 := lua.NewState()
		L2 := lua.NewState()

		// Create two select operations
		select1 := &selectOp{
			task: L1,
			cases: []*op{
				{
					kind: sendOp,
					ch:   ch,
					task: L1,
				},
			},
		}
		select1.cases[0].selectOp = select1

		select2 := &selectOp{
			task: L2,
			cases: []*op{
				{
					kind: sendOp,
					ch:   ch,
					task: L2,
				},
			},
		}
		select2.cases[0].selectOp = select2

		// Add both operations to channel
		ch.senders.PushBack(select1.cases[0])
		ch.senders.PushBack(select2.cases[0])
		ch.size += 2

		// Release first select
		releases := release(select1.cases[0])
		if len(releases) != 1 {
			t.Error("should only release one channel")
		}

		// Verify only first operation was removed
		if ch.senders.Len() != 1 {
			t.Error("should have one remaining operation")
		}
		if ch.size != 1 {
			t.Error("channel size should be decremented once")
		}

		// Verify remaining operation is from select2
		remainingOp := ch.senders.Front().Value.(*op)
		if remainingOp.selectOp != select2 {
			t.Error("wrong operation was removed")
		}
	})
}

func TestChannelBlockingScenarios(t *testing.T) {
	t.Run("blocking with multiple operations", func(t *testing.T) {
		ch := newChannel(0)
		L1 := lua.NewState()
		L2 := lua.NewState()
		L3 := lua.NewState()

		// Add multiple blocked senders
		next1 := ch.send(L1, lua.LString("test1"), nil)
		next2 := ch.send(L2, lua.LString("test2"), nil)

		if !next1.yields || !next2.yields {
			t.Error("senders should block")
		}
		if len(next1.block) != 1 || len(next2.block) != 1 {
			t.Error("each operation should block on the channel")
		}

		// Receiver should unblock first sender only
		next := ch.receive(L3, nil)
		if len(next.next) != 2 {
			t.Error("should wake up receiver and first sender only")
		}

		// Verify second sender still blocked
		if ch.senders.Len() != 1 {
			t.Error("should have one sender still blocked")
		}

		remainingSend := ch.senders.Front().Value.(*op)
		if remainingSend.task != L2 {
			t.Error("wrong sender was unblocked")
		}
	})

	t.Run("blocking select with multiple channels", func(t *testing.T) {
		ch1 := newChannel(0)
		ch2 := newChannel(0)
		L1 := lua.NewState()
		L2 := lua.NewState()

		// Create select operation watching both channels
		selectOp := &selectOp{
			task: L1,
			cases: []*op{
				{
					kind: receiveOp,
					ch:   ch1,
					task: L1,
				},
				{
					kind: receiveOp,
					ch:   ch2,
					task: L1,
				},
			},
		}
		selectOp.cases[0].selectOp = selectOp
		selectOp.cases[1].selectOp = selectOp

		// Block select operation on both channels
		next1 := ch1.receive(L1, selectOp)
		if !next1.yields {
			t.Error("select should block initially")
		}

		// Send on first channel should unblock select
		next2 := ch1.send(L2, lua.LString("test"), nil)
		if len(next2.next) != 2 {
			t.Error("should wake up both sender and select")
		}

		// Verify second channel was cleaned up
		if ch2.receivers.Len() != 0 {
			t.Error("select operation should be removed from other channel")
		}
	})
}
