package channel

import (
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
)

// channelOps handles core channel operations
type channelOps struct {
	senders   *queueMapper
	receivers *queueMapper
}

func newChannelOps() *channelOps {
	return &channelOps{
		senders:   newQueueMapper(),
		receivers: newQueueMapper(),
	}
}

func (ops *channelOps) getQueueSize(ch *Channel, isSender bool) int {
	if isSender {
		return ops.senders.getQueueSize(ch)
	}
	return ops.receivers.getQueueSize(ch)
}

func (ops *channelOps) tryReceive(ch *Channel) (lua.LValue, bool) {
	// Try buffered value first
	if value, ok := ch.receive(); ok {
		return value, true
	}

	// Return nil for closed channel
	if ch.closed {
		return nil, false
	}

	return nil, false
}

func (ops *channelOps) trySend(ch *Channel, value lua.LValue) bool {
	if ch.closed {
		return false
	}

	// Try buffer for buffered channels
	if ch.capacity > 0 && !ch.isFull() {
		return ch.send(value)
	}

	return false
}

// findReceiver handles queued receiver operations
func (ops *channelOps) findReceiver(ch *Channel, value lua.LValue) *engine.Task {
	receiver := ops.receivers.dequeue(ch)
	if receiver == nil {
		return nil
	}

	if receiver.selectOp == nil {
		receiver.task.Resumed = []lua.LValue{value, lua.LBool(true)}
	} else {
		receiver.task.Resumed = []lua.LValue{receiver.selectOp.caseResult(
			receiver.task.Thread(), ch, value, true,
		)}
	}

	task := receiver.task
	receiver.reset()
	pendingPool.Put(receiver)
	return task
}

// findSender handles queued sender operations
func (ops *channelOps) findSender(ch *Channel) (lua.LValue, *engine.Task) {
	sender := ops.senders.dequeue(ch)
	if sender == nil {
		return nil, nil
	}

	value := sender.op.value
	if sender.selectOp == nil {
		sender.task.Resumed = []lua.LValue{lua.LBool(true)}
	} else {
		sender.task.Resumed = []lua.LValue{sender.selectOp.caseResult(
			sender.task.Thread(), ch, nil, true,
		)}
	}

	task := sender.task
	sender.reset()
	pendingPool.Put(sender)
	return value, task
}

func (ops *channelOps) pushNamed(name string, value lua.LValue) (*engine.Task, error) {
	if receiver := ops.receivers.dequeueNamed(name); receiver != nil {
		receiver.task.Resumed = []lua.LValue{value, lua.LBool(true)}
		return receiver.task, nil
	}
	return nil, fmt.Errorf("no receiver found for channel %s", name)
}

// queueOperation queues an operation for later processing
func (ops *channelOps) queueOperation(task *engine.Task, op *chanOperation, selectOp *selectOperation) {
	node := newPendingOp(task, op, selectOp)

	if op.dir == chanSend {
		ops.senders.enqueue(op.ch, node)
	} else {
		ops.receivers.enqueue(op.ch, node)
	}
}

// getOpenChannels returns list of active channels being listened to
func (ops *channelOps) getOpenChannels() []string {
	var names []string
	for name := range ops.receivers.named {
		names = append(names, name)
	}
	return names
}

func (ops *channelOps) cleanup() {
	ops.senders.clear()
	ops.receivers.clear()
}
