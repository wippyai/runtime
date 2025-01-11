package channel

import (
	"github.com/ponyruntime/pony/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
	"testing"
)

func TestChannelTrySend(t *testing.T) {
	t.Run("send to unbuffered channel with waiting receiver", func(t *testing.T) {
		ch := newChannel(0)
		receiverTask := &engine.Task{}
		value := lua.LString("test-value")

		// Setup waiting receiver
		match := ch.tryReceive(receiverTask, nil)
		if !match.yields {
			t.Error("receiver should yield when no sender available")
		}

		// Try sending
		senderTask := &engine.Task{}
		match = ch.trySend(senderTask, value, nil)

		if !match.yields {
			t.Error("expected yield for synchronization")
		}
		if len(match.wakeTasks) != 2 {
			t.Errorf("expected 2 tasks to wake, got %d", len(match.wakeTasks))
		}
		if match.value != value {
			t.Errorf("expected value %v, got %v", value, match.value)
		}
	})

	t.Run("send to buffered channel", func(t *testing.T) {
		ch := newChannel(1)
		value := lua.LString("test-value")
		senderTask := &engine.Task{}

		match := ch.trySend(senderTask, value, nil)

		if match.yields {
			t.Error("should not yield when buffer available")
		}
		if ch.size != 1 {
			t.Errorf("expected size 1, got %d", ch.size)
		}

		// Second send should block
		match = ch.trySend(senderTask, value, nil)
		if !match.yields {
			t.Error("should yield when buffer full")
		}
		if match.wakeTasks != nil {
			t.Error("expected no tasks to wake")
		}
	})
}

func TestChannelTryReceive(t *testing.T) {
	t.Run("receive from unbuffered channel with waiting sender", func(t *testing.T) {
		ch := newChannel(0)
		value := lua.LString("test-value")
		senderTask := &engine.Task{}

		// Setup waiting sender
		match := ch.trySend(senderTask, value, nil)
		if !match.yields {
			t.Error("sender should yield when no receiver available")
		}
		if match.wakeTasks != nil {
			t.Error("expected no tasks to wake")
		}

		// Try receiving
		receiverTask := &engine.Task{}
		match = ch.tryReceive(receiverTask, nil)

		if !match.yields {
			t.Error("expected yield for synchronization")
		}
		if len(match.wakeTasks) != 2 {
			t.Errorf("expected 2 tasks to wake, got %d", len(match.wakeTasks))
		}
		if match.value != value {
			t.Errorf("expected value %v, got %v", value, match.value)
		}
	})

	t.Run("receive from buffered channel", func(t *testing.T) {
		ch := newChannel(1)
		value := lua.LString("test-value")
		senderTask := &engine.Task{}

		// First send should succeed without blocking
		match := ch.trySend(senderTask, value, nil)
		if match.yields {
			t.Error("send should not yield with available buffer")
		}

		// Try receiving
		receiverTask := &engine.Task{}
		match = ch.tryReceive(receiverTask, nil)

		if match.yields {
			t.Error("should not yield when value available")
		}
		if match.value != value {
			t.Errorf("expected value %v, got %v", value, match.value)
		}
		if match.wakeTasks != nil {
			t.Error("expected no tasks to wake")
		}
		if ch.size != 0 {
			t.Errorf("expected size 0, got %d", ch.size)
		}
	})
}

func TestChannelClose(t *testing.T) {
	t.Run("close empty channel", func(t *testing.T) {
		ch := newChannel(0)
		match := ch.close()

		if !match.closed {
			t.Error("match.closed should be true")
		}
		if !match.yields {
			t.Error("close should always yield for synchronization")
		}
		if len(match.wakeTasks) != 0 {
			t.Error("should have no tasks to wake")
		}
	})

	t.Run("close with waiting receivers", func(t *testing.T) {
		ch := newChannel(0)
		receiver1 := &engine.Task{}
		receiver2 := &engine.Task{}

		ch.tryReceive(receiver1, nil)
		ch.tryReceive(receiver2, nil)

		match := ch.close()

		if !match.closed {
			t.Error("match.closed should be true")
		}
		if !match.yields {
			t.Error("should yield to wake tasks")
		}
		if len(match.wakeTasks) != 2 {
			t.Errorf("expected 2 tasks to wake, got %d", len(match.wakeTasks))
		}
		if ch.size != 0 {
			t.Errorf("channel size should be 0, got %d", ch.size)
		}
	})

	t.Run("close with waiting senders", func(t *testing.T) {
		ch := newChannel(0)
		sender1 := &engine.Task{}
		sender2 := &engine.Task{}

		ch.trySend(sender1, lua.LString("value1"), nil)
		ch.trySend(sender2, lua.LString("value2"), nil)

		match := ch.close()

		if !match.closed {
			t.Error("match.closed should be true")
		}
		if !match.yields {
			t.Error("should yield to wake tasks")
		}
		if len(match.wakeTasks) != 2 {
			t.Errorf("expected 2 tasks to wake, got %d", len(match.wakeTasks))
		}
	})

	t.Run("send on closed channel", func(t *testing.T) {
		ch := newChannel(0)
		ch.close()

		match := ch.trySend(&engine.Task{}, lua.LString("value"), nil)

		if !match.closed {
			t.Error("match.closed should be true")
		}
		if match.yields {
			t.Error("should not yield on closed channel")
		}
		if match.wakeTasks != nil {
			t.Error("should have no tasks to wake")
		}
	})

	t.Run("receive on closed channel", func(t *testing.T) {
		ch := newChannel(0)
		ch.close()

		match := ch.tryReceive(&engine.Task{}, nil)

		if !match.closed {
			t.Error("match.closed should be true")
		}
		if match.yields {
			t.Error("should not yield on closed channel")
		}
		if match.wakeTasks != nil {
			t.Error("should have no tasks to wake")
		}
	})

	t.Run("close buffered channel with values", func(t *testing.T) {
		ch := newChannel(2)
		ch.trySend(nil, lua.LString("value1"), nil) // First buffered value
		ch.trySend(nil, lua.LString("value2"), nil) // Second buffered value

		match := ch.close()

		if !match.closed {
			t.Error("match.closed should be true")
		}
		if !match.yields {
			t.Error("close should always yield for synchronization")
		}
		if len(match.wakeTasks) != 0 {
			t.Errorf("expected no tasks to wake with only buffered values, got %d", len(match.wakeTasks))
		}
		if ch.size != 0 {
			t.Errorf("channel should be empty after close, got size %d", ch.size)
		}
	})

	t.Run("double close", func(t *testing.T) {
		ch := newChannel(0)
		ch.close()
		match := ch.close()

		if !match.closed {
			t.Error("match.closed should be true")
		}
		if match.yields {
			t.Error("double close should not yield")
		}
		if match.wakeTasks != nil {
			t.Error("should have no tasks to wake")
		}
	})
}

func TestChannelTaskWakeOrder(t *testing.T) {
	t.Run("unbuffered channel wake order - receiver first", func(t *testing.T) {
		ch := newChannel(0)
		receiverTask := &engine.Task{}
		senderTask := &engine.Task{}

		// Setup waiting receiver first
		match := ch.tryReceive(receiverTask, nil)
		if !match.yields {
			t.Fatal("receiver should yield")
		}

		// Then send
		match = ch.trySend(senderTask, lua.LString("test"), nil)
		if len(match.wakeTasks) != 2 {
			t.Fatalf("expected 2 tasks to wake, got %d", len(match.wakeTasks))
		}

		// Verify order: [receiver, sender]
		if match.wakeTasks[0] != receiverTask {
			t.Error("first task should be receiver")
		}
		if match.wakeTasks[1] != senderTask {
			t.Error("second task should be sender")
		}
	})

	t.Run("unbuffered channel wake order - sender first", func(t *testing.T) {
		ch := newChannel(0)
		senderTask := &engine.Task{}
		receiverTask := &engine.Task{}

		// Setup waiting sender first
		match := ch.trySend(senderTask, lua.LString("test"), nil)
		if !match.yields {
			t.Fatal("sender should yield")
		}

		// Then receive
		match = ch.tryReceive(receiverTask, nil)
		if len(match.wakeTasks) != 2 {
			t.Fatalf("expected 2 tasks to wake, got %d", len(match.wakeTasks))
		}

		// Verify order: [sender, receiver]
		if match.wakeTasks[0] != senderTask {
			t.Error("first task should be sender")
		}
		if match.wakeTasks[1] != receiverTask {
			t.Error("second task should be receiver")
		}
	})

	t.Run("overlapped operations wake order", func(t *testing.T) {
		ch := newChannel(0)
		sender1 := &engine.Task{}
		sender2 := &engine.Task{}
		receiver1 := &engine.Task{}

		// First sender queues
		match := ch.trySend(sender1, lua.LString("test1"), nil)
		if !match.yields {
			t.Fatal("first send should yield")
		}

		// Second sender queues
		match = ch.trySend(sender2, lua.LString("test2"), nil)
		if !match.yields {
			t.Fatal("second send should yield")
		}

		// Receiver arrives - should match with first sender
		match = ch.tryReceive(receiver1, nil)
		if !match.yields {
			t.Fatal("receive should yield")
		}
		if len(match.wakeTasks) != 2 {
			t.Fatalf("expected 2 tasks to wake, got %d", len(match.wakeTasks))
		}
		if match.wakeTasks[0] != sender1 {
			t.Error("first task should be first sender")
		}
		if match.wakeTasks[1] != receiver1 {
			t.Error("second task should be receiver")
		}
		if match.value.String() != "test1" {
			t.Errorf("expected value test1, got %s", match.value.String())
		}
	})

	t.Run("close with pending sender", func(t *testing.T) {
		ch := newChannel(0)
		sender := &engine.Task{}

		// Queue sender
		match := ch.trySend(sender, lua.LString("test"), nil)
		if !match.yields {
			t.Fatal("send should yield")
		}

		// Close should wake the sender
		match = ch.close()
		if len(match.wakeTasks) != 1 {
			t.Fatalf("expected 1 task to wake, got %d", len(match.wakeTasks))
		}
		if match.wakeTasks[0] != sender {
			t.Error("wake task should be sender")
		}
		if !match.closed {
			t.Error("match should indicate channel closed")
		}
	})

	t.Run("close with pending receivers", func(t *testing.T) {
		ch := newChannel(0)
		receiver1 := &engine.Task{}
		receiver2 := &engine.Task{}

		match := ch.tryReceive(receiver1, nil)
		if !match.yields {
			t.Fatal("first receive should yield")
		}

		match = ch.tryReceive(receiver2, nil)
		if !match.yields {
			t.Fatal("second receive should yield")
		}

		match = ch.close()
		if len(match.wakeTasks) != 2 {
			t.Fatalf("expected 2 tasks to wake, got %d", len(match.wakeTasks))
		}
		if match.wakeTasks[0] != receiver1 {
			t.Error("first task should be first receiver")
		}
		if match.wakeTasks[1] != receiver2 {
			t.Error("second task should be second receiver")
		}
		if !match.closed {
			t.Error("match should indicate channel closed")
		}
	})

	t.Run("buffered channel preserves operation order", func(t *testing.T) {
		ch := newChannel(1)
		senderTask := &engine.Task{}

		// First send succeeds (buffered)
		match := ch.trySend(nil, lua.LString("value1"), nil)
		if match.yields {
			t.Fatal("first send should not yield with buffer")
		}

		// Second send blocks (buffer full)
		match = ch.trySend(senderTask, lua.LString("value2"), nil)
		if !match.yields {
			t.Fatal("second send should yield when buffer full")
		}

		// Receive should get first (buffered) value
		receiverTask := &engine.Task{}
		match = ch.tryReceive(receiverTask, nil)

		// Should not yield or wake tasks - just return buffered value
		if match.yields {
			t.Error("receive of buffered value should not yield")
		}
		if match.wakeTasks != nil {
			t.Error("receive of buffered value should not wake tasks")
		}

		// Value should be first sent value
		if match.value.String() != "value1" {
			t.Errorf("expected value1, got %s", match.value.String())
		}
	})
}

func TestChannelHelpers(t *testing.T) {
	t.Run("empty and full state", func(t *testing.T) {
		ch := newChannel(2)

		if !ch.isEmpty() {
			t.Error("new channel should be empty")
		}
		if ch.isFull() {
			t.Error("new channel should not be full")
		}

		ch.trySend(nil, lua.LString("value1"), nil)
		if ch.isEmpty() {
			t.Error("channel with one value should not be empty")
		}
		if ch.isFull() {
			t.Error("channel with one value should not be full")
		}

		ch.trySend(nil, lua.LString("value2"), nil)
		if ch.isEmpty() {
			t.Error("channel with two values should not be empty")
		}
		if !ch.isFull() {
			t.Error("channel at capacity should be full")
		}
	})

	t.Run("named channel", func(t *testing.T) {
		ch := newChannel(1)
		if ch.isNamed() {
			t.Error("default channel should not be named")
		}

		named := Named("test", 1)
		if !named.isNamed() {
			t.Error("Named() channel should be named")
		}
		if named.name != "test" {
			t.Errorf("expected name 'test', got '%s'", named.name)
		}
	})

	t.Run("can send states", func(t *testing.T) {
		ch := newChannel(1)

		// Empty channel should allow send
		if !ch.canSend() {
			t.Error("should be able to send to empty channel")
		}

		// Full unbuffered channel with no receiver shouldn't allow send
		unbuffered := newChannel(0)
		if unbuffered.canSend() {
			t.Error("shouldn't be able to send to unbuffered channel without receiver")
		}

		// Full unbuffered channel with receiver should allow send
		receiver := &engine.Task{}
		unbuffered.tryReceive(receiver, nil)
		if !unbuffered.canSend() {
			t.Error("should be able to send to unbuffered channel with receiver")
		}

		// Full channel shouldn't allow send
		ch.trySend(nil, lua.LString("value"), nil)
		if ch.canSend() {
			t.Error("shouldn't be able to send to full channel")
		}

		// Closed channel shouldn't allow send
		ch = newChannel(1)
		ch.close()
		if ch.canSend() {
			t.Error("shouldn't be able to send to closed channel")
		}
	})

	t.Run("can receive states", func(t *testing.T) {
		ch := newChannel(1)

		// Empty channel without sender shouldn't allow receive
		if ch.canReceive() {
			t.Error("shouldn't be able to receive from empty channel")
		}

		// Channel with queued value should allow receive
		ch.trySend(nil, lua.LString("value"), nil)
		if !ch.canReceive() {
			t.Error("should be able to receive when value available")
		}

		// Channel with sender should allow receive
		ch = newChannel(0)
		sender := &engine.Task{}
		ch.trySend(sender, lua.LString("value"), nil)
		if !ch.canReceive() {
			t.Error("should be able to receive when sender available")
		}
	})
}

func TestChannelSelect(t *testing.T) {
	t.Run("select send - no receivers", func(t *testing.T) {
		ch := newChannel(0)
		task := &engine.Task{}
		selectOp := &selectOp{
			task: task,
			cases: []*op{{
				kind:     sendOp,
				ch:       ch,
				value:    lua.LString("test"),
				selectOp: nil, // Will be set below
			}},
		}
		selectOp.cases[0].selectOp = selectOp

		match := ch.trySend(task, lua.LString("test"), selectOp)
		if !match.yields {
			t.Fatal("select send should yield when no receiver")
		}
		if match.wakeTasks != nil {
			t.Error("no tasks should wake")
		}
	})

	t.Run("select send - has receiver", func(t *testing.T) {
		ch := newChannel(0)
		receiverTask := &engine.Task{}
		senderTask := &engine.Task{}

		// Setup receiver first
		match := ch.tryReceive(receiverTask, nil)
		if !match.yields {
			t.Fatal("receiver should yield")
		}

		// Try select send
		selectOp := &selectOp{
			task: senderTask,
			cases: []*op{{
				kind:     sendOp,
				ch:       ch,
				value:    lua.LString("test"),
				selectOp: nil,
			}},
		}
		selectOp.cases[0].selectOp = selectOp

		match = ch.trySend(senderTask, lua.LString("test"), selectOp)
		if !match.yields {
			t.Fatal("select send should yield for sync")
		}
		if len(match.wakeTasks) != 2 {
			t.Fatalf("expected 2 tasks to wake, got %d", len(match.wakeTasks))
		}
		if match.wakeTasks[0] != receiverTask {
			t.Error("first wake task should be receiver")
		}
	})

	t.Run("select receive - no senders", func(t *testing.T) {
		ch := newChannel(0)
		task := &engine.Task{}
		selectOp := &selectOp{
			task: task,
			cases: []*op{{
				kind:     receiveOp,
				ch:       ch,
				selectOp: nil,
			}},
		}
		selectOp.cases[0].selectOp = selectOp

		match := ch.tryReceive(task, selectOp)
		if !match.yields {
			t.Fatal("select receive should yield when no sender")
		}
		if match.wakeTasks != nil {
			t.Error("no tasks should wake")
		}
	})

	t.Run("select receive - has sender", func(t *testing.T) {
		ch := newChannel(0)
		senderTask := &engine.Task{}
		receiverTask := &engine.Task{}

		// Setup sender first
		match := ch.trySend(senderTask, lua.LString("test"), nil)
		if !match.yields {
			t.Fatal("sender should yield")
		}

		// Try select receive
		selectOp := &selectOp{
			task: receiverTask,
			cases: []*op{{
				kind:     receiveOp,
				ch:       ch,
				selectOp: nil,
			}},
		}
		selectOp.cases[0].selectOp = selectOp

		match = ch.tryReceive(receiverTask, selectOp)
		if !match.yields {
			t.Fatal("select receive should yield for sync")
		}
		if len(match.wakeTasks) != 2 {
			t.Fatalf("expected 2 tasks to wake, got %d", len(match.wakeTasks))
		}
		if match.wakeTasks[0] != senderTask {
			t.Error("first wake task should be sender")
		}
		if match.value.String() != "test" {
			t.Errorf("expected value 'test', got '%s'", match.value.String())
		}
	})

	t.Run("discard select op", func(t *testing.T) {
		ch := newChannel(0)
		task := &engine.Task{}
		selectOp := &selectOp{
			task: task,
			cases: []*op{{
				kind:     sendOp,
				ch:       ch,
				value:    lua.LString("test"),
				selectOp: nil,
			}},
		}
		selectOp.cases[0].selectOp = selectOp

		// Queue select op
		match := ch.trySend(task, lua.LString("test"), selectOp)
		if !match.yields {
			t.Fatal("select send should yield")
		}

		// Discard select op
		ch.discardSelect(selectOp)

		// Verify channel state
		if !ch.isEmpty() {
			t.Error("channel should be empty after discard")
		}

		// Try new operation
		receiverTask := &engine.Task{}
		match = ch.tryReceive(receiverTask, nil)
		if !match.yields {
			t.Error("channel should be empty after discard")
		}
		if match.value != nil {
			t.Error("no value should be available after discard")
		}
	})

	t.Run("select with buffered channel", func(t *testing.T) {
		ch := newChannel(1)
		task := &engine.Task{}
		selectOp := &selectOp{
			task: task,
			cases: []*op{{
				kind:     sendOp,
				ch:       ch,
				value:    lua.LString("test"),
				selectOp: nil,
			}},
		}
		selectOp.cases[0].selectOp = selectOp

		// First send should succeed immediately
		match := ch.trySend(task, lua.LString("test"), selectOp)
		if match.yields {
			t.Error("first select send should not yield")
		}

		// Second send should block
		match = ch.trySend(task, lua.LString("test2"), selectOp)
		if !match.yields {
			t.Error("second select send should yield")
		}
	})
}

func TestChannelSelectMatching(t *testing.T) {
	t.Run("select receiver matches with normal sender", func(t *testing.T) {
		ch := newChannel(0)
		receiverTask := &engine.Task{}
		senderTask := &engine.Task{}

		// Setup select receiver first
		selectOp := &selectOp{
			task: receiverTask,
			cases: []*op{{
				kind:     receiveOp,
				ch:       ch,
				selectOp: nil, // Will be set below
			}},
		}
		selectOp.cases[0].selectOp = selectOp

		match := ch.tryReceive(receiverTask, selectOp)
		if !match.yields {
			t.Fatal("select receive should yield when no sender")
		}

		// Try normal send - should match with select receiver
		value := lua.LString("test-value")
		match = ch.trySend(senderTask, value, nil)

		if !match.yields {
			t.Error("expected yield for synchronization")
		}
		if match.selectOp != selectOp {
			t.Error("expected selectOp to be returned")
		}
		if len(match.wakeTasks) != 2 {
			t.Errorf("expected 2 tasks to wake, got %d", len(match.wakeTasks))
		}
		if match.wakeTasks[0] != receiverTask {
			t.Error("first task should be select receiver")
		}
		if match.wakeTasks[1] != senderTask {
			t.Error("second task should be sender")
		}
		if match.value != value {
			t.Errorf("expected value %v, got %v", value, match.value)
		}
	})

	t.Run("select sender matches with normal receiver", func(t *testing.T) {
		ch := newChannel(0)
		senderTask := &engine.Task{}
		receiverTask := &engine.Task{}
		value := lua.LString("test-value")

		// Setup select sender first
		selectOp := &selectOp{
			task: senderTask,
			cases: []*op{{
				kind:     sendOp,
				ch:       ch,
				value:    value,
				selectOp: nil, // Will be set below
			}},
		}
		selectOp.cases[0].selectOp = selectOp

		match := ch.trySend(senderTask, value, selectOp)
		if !match.yields {
			t.Fatal("select send should yield when no receiver")
		}

		// Try normal receive - should match with select sender
		match = ch.tryReceive(receiverTask, nil)

		if !match.yields {
			t.Error("expected yield for synchronization")
		}
		if match.selectOp != selectOp {
			t.Error("expected selectOp to be returned")
		}
		if len(match.wakeTasks) != 2 {
			t.Errorf("expected 2 tasks to wake, got %d", len(match.wakeTasks))
		}
		if match.wakeTasks[0] != senderTask {
			t.Error("first task should be select sender")
		}
		if match.wakeTasks[1] != receiverTask {
			t.Error("second task should be receiver")
		}
		if match.value != value {
			t.Errorf("expected value %v, got %v", value, match.value)
		}
	})
}

func TestDiscardSelectOperations(t *testing.T) {
	t.Run("discard select with multiple receivers", func(t *testing.T) {
		ch := newChannel(0)
		task1, task2 := &engine.Task{}, &engine.Task{}

		// Create two select operations for different tasks
		selectOp1 := &selectOp{
			task: task1,
			cases: []*op{{
				kind:     receiveOp,
				ch:       ch,
				selectOp: nil,
			}},
		}
		selectOp1.cases[0].selectOp = selectOp1

		selectOp2 := &selectOp{
			task: task2,
			cases: []*op{{
				kind:     receiveOp,
				ch:       ch,
				selectOp: nil,
			}},
		}
		selectOp2.cases[0].selectOp = selectOp2

		// Queue both select receivers
		match := ch.tryReceive(task1, selectOp1)
		if !match.yields {
			t.Fatal("first select receive should yield")
		}

		match = ch.tryReceive(task2, selectOp2)
		if !match.yields {
			t.Fatal("second select receive should yield")
		}

		// Verify initial state
		if ch.receivers.Len() != 2 {
			t.Errorf("expected 2 receivers, got %d", ch.receivers.Len())
		}

		// Discard selectOp1
		ch.discardSelect(selectOp1)

		// Verify only selectOp1 was removed
		if ch.receivers.Len() != 1 {
			t.Errorf("expected 1 receiver after discard, got %d", ch.receivers.Len())
		}

		// Verify remaining receiver is selectOp2
		remainingOp := ch.receivers.Front().Value.(*op)
		if remainingOp.selectOp != selectOp2 {
			t.Error("wrong select operation was removed")
		}

		// Discard selectOp2
		ch.discardSelect(selectOp2)

		// Verify channel is empty
		if ch.receivers.Len() != 0 {
			t.Errorf("expected 0 receivers after all discards, got %d", ch.receivers.Len())
		}
	})

	t.Run("discard select with mixed operations", func(t *testing.T) {
		ch := newChannel(0)
		task1, task2 := &engine.Task{}, &engine.Task{}

		// Create select operations that we'll queue
		selectOp := &selectOp{
			task: task1,
			cases: []*op{
				{
					kind:     receiveOp,
					ch:       ch,
					selectOp: nil,
				},
				{
					kind:     sendOp,
					ch:       ch,
					value:    lua.LString("test"),
					selectOp: nil,
				},
			},
		}
		selectOp.cases[0].selectOp = selectOp
		selectOp.cases[1].selectOp = selectOp

		// Queue one normal receiver and one select receiver
		ch.tryReceive(task2, nil)      // Normal receiver
		ch.tryReceive(task1, selectOp) // Select receiver

		if ch.receivers.Len() != 2 {
			t.Fatalf("expected 2 receivers initially, got %d", ch.receivers.Len())
		}

		// Count select ops before discard
		selectOpsCount := 0
		for e := ch.receivers.Front(); e != nil; e = e.Next() {
			if e.Value.(*op).selectOp != nil {
				selectOpsCount++
			}
		}
		if selectOpsCount != 1 {
			t.Errorf("expected 1 select operation, got %d", selectOpsCount)
		}

		// Discard select operations
		ch.discardSelect(selectOp)

		// Verify only select operations were removed, leaving normal ones
		if ch.receivers.Len() != 1 {
			t.Errorf("expected 1 receiver after discard, got %d", ch.receivers.Len())
		}

		// Verify remaining operation is non-select
		remainingRecv := ch.receivers.Front()
		if remainingRecv == nil {
			t.Fatal("receiver list is empty")
		}

		if remainingRecv.Value.(*op).selectOp != nil {
			t.Error("remaining receiver should not be select operation")
		}

		// Queue new operations to verify channel still works
		value := lua.LString("test-after-discard")
		senderTask := &engine.Task{}
		match := ch.trySend(senderTask, value, nil)

		if !match.yields {
			t.Error("send should yield to sync with receiver")
		}
		if len(match.wakeTasks) != 2 {
			t.Errorf("expected 2 tasks to wake, got %d", len(match.wakeTasks))
		}
		if match.wakeTasks[0] != task2 {
			t.Error("first task should be the remaining normal receiver")
		}
	})

	t.Run("discard select with buffered channel", func(t *testing.T) {
		ch := newChannel(2)
		task := &engine.Task{}

		selectOp := &selectOp{
			task: task,
			cases: []*op{{
				kind:     sendOp,
				ch:       ch,
				value:    lua.LString("test"),
				selectOp: nil,
			}},
		}
		selectOp.cases[0].selectOp = selectOp

		// Fill buffer with one normal value and one select value
		ch.trySend(nil, lua.LString("normal"), nil)
		ch.trySend(task, lua.LString("select"), selectOp)

		initialSize := ch.size

		// Discard select operation
		ch.discardSelect(selectOp)

		// Verify size was decremented
		if ch.size != initialSize-1 {
			t.Errorf("expected size %d, got %d", initialSize-1, ch.size)
		}

		// Try receive - should get normal value
		match := ch.tryReceive(&engine.Task{}, nil)
		if match.value.String() != "normal" {
			t.Errorf("expected to receive normal value, got %s", match.value.String())
		}

		// Channel should now be empty
		if !ch.isEmpty() {
			t.Error("channel should be empty after receiving normal value")
		}
	})

	t.Run("discard non-existent select", func(t *testing.T) {
		ch := newChannel(0)
		task := &engine.Task{}

		// Queue a normal operation
		ch.tryReceive(task, nil)
		initialReceivers := ch.receivers.Len()

		// Create and discard a select that was never queued
		selectOp := &selectOp{
			task: task,
			cases: []*op{{
				kind:     receiveOp,
				ch:       ch,
				selectOp: nil,
			}},
		}
		selectOp.cases[0].selectOp = selectOp

		ch.discardSelect(selectOp)

		// Verify nothing changed
		if ch.receivers.Len() != initialReceivers {
			t.Errorf("expected %d receivers, got %d", initialReceivers, ch.receivers.Len())
		}
	})
}
