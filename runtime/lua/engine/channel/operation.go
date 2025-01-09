package channel

import (
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
)

type chanOp int

const (
	chanSend chanOp = iota
	chanReceive
	chanClose
)

// carries chan operation context over to the scheduler
type pendingOp struct {
	task     *engine.Task
	op       *chanOperation
	next     *pendingOp
	selectOp *selectSwitch // If this op is part of a select
}

func (p *pendingOp) reset() {
	p.task = nil
	p.op = nil
	p.next = nil
	p.selectOp = nil
}

// chanOperation sent via yields to coordinate channel communication
type chanOperation struct {
	opType chanOp
	ch     *Channel
	value  lua.LValue
}

func (y *chanOperation) String() string {
	switch y.opType {
	case chanSend:
		return fmt.Sprintf("channel.send{value=%+v}", y.value)
	case chanReceive:
		return fmt.Sprintf("channel.receive")
	case chanClose:
		return fmt.Sprintf("channel.close")
	}
	return "unknown"
}

func (y *chanOperation) Type() lua.LValueType {
	return lua.LTUserData
}

// OperationHandler handles channel operations
type OperationHandler struct {
	senders   *queueMapper
	receivers *queueMapper
	inbox     *inbox
}

// NewOperationHandler creates a new operation handler
func NewOperationHandler() *OperationHandler {
	return &OperationHandler{
		senders:   newQueueMapper(),
		receivers: newQueueMapper(),
		inbox:     newInbox(),
	}
}

// HandleOperation processes a channel operation and returns resulting tasks
func (h *OperationHandler) HandleOperation(task *engine.Task, op *chanOperation) []*engine.Task {
	switch op.opType {
	case chanSend:
		return h.handleSend(task, op)
	case chanReceive:
		return h.handleReceive(task, op)
	case chanClose:
		return h.handleClose(task, op)
	default:
		return nil
	}
}

// handleSelect processes a select operation
func (h *OperationHandler) handleSelect(task *engine.Task, op *selectSwitch) []*engine.Task {
	return h.registerSelectCases(task, op)
}

func (h *OperationHandler) handleSend(task *engine.Task, op *chanOperation) []*engine.Task {
	ch := op.ch
	if ch.closed {
		return h.completeTask(task, lua.LNil)
	}

	// Try buffer first
	if ch.capacity > 0 && !ch.isFull() {
		if ch.send(op.value) {
			return h.completeTask(task, lua.LBool(true))
		}
	}

	// Try matching with receiver
	if receiver := h.receivers.dequeue(ch); receiver != nil {
		return h.completeSendReceivePair(task, receiver.task, op.value)
	}

	// Queue sender
	h.queueOperation(h.senders, ch, task, op, nil)
	return nil
}

func (h *OperationHandler) handleReceive(task *engine.Task, op *chanOperation) []*engine.Task {
	ch := op.ch

	// Try buffered value
	if value, ok := ch.receive(); ok {
		return h.completeTask(task, value, lua.LBool(true))
	}

	if ch.closed {
		return h.completeTask(task, lua.LNil, lua.LBool(false))
	}

	// Try matching with sender
	if sender := h.senders.dequeue(ch); sender != nil {
		return h.completeSendReceivePair(sender.task, task, sender.op.value)
	}

	// Handle named channels
	if ch.IsNamed() {
		h.queueOperation(nil, ch, task, op, h.inbox)
		return nil
	}

	// Queue receiver
	h.queueOperation(h.receivers, ch, task, op, nil)
	return nil
}

func (h *OperationHandler) handleClose(task *engine.Task, op *chanOperation) []*engine.Task {
	ch := op.ch
	ch.closed = true

	result := h.completeTask(task, lua.LBool(true))

	// Handle pending senders
	for sender := h.senders.dequeue(ch); sender != nil; sender = h.senders.dequeue(ch) {
		if sender.selectOp == nil {
			result = append(result, h.completeTask(sender.task, lua.LNil)...)
		} else {
			sender.task.RaiseError = fmt.Errorf("channel closed")
			result = append(result, sender.task)
		}
		sender.reset()
		pendingPool.Put(sender)
	}

	// Handle pending receivers
	for receiver := h.receivers.dequeue(ch); receiver != nil; receiver = h.receivers.dequeue(ch) {
		if value, ok := ch.receive(); ok {
			if receiver.selectOp == nil {
				result = append(result, h.completeTask(receiver.task, value, lua.LBool(true))...)
			} else {
				result = append(result, h.completeSelectCase(receiver.task, receiver.selectOp, ch, value, true)...)
			}
		} else {
			if receiver.selectOp == nil {
				result = append(result, h.completeTask(receiver.task, lua.LNil, lua.LBool(false))...)
			} else {
				result = append(result, h.completeSelectCase(receiver.task, receiver.selectOp, ch, nil, false)...)
			}
		}
		receiver.reset()
		pendingPool.Put(receiver)
	}

	return result
}

// Helper methods

func (h *OperationHandler) queueOperation(queue *queueMapper, ch *Channel, task *engine.Task, op *chanOperation, inbox *inbox) {
	node := pendingPool.Get().(*pendingOp)
	node.task = task
	node.op = op

	if inbox != nil {
		inbox.addReceiver(ch.Name(), node)
	} else {
		queue.enqueue(ch, node)
	}
}

func (h *OperationHandler) completeTask(task *engine.Task, values ...lua.LValue) []*engine.Task {
	task.Resumed = values
	return []*engine.Task{task}
}

func (h *OperationHandler) completeSendReceivePair(senderTask, receiverTask *engine.Task, value lua.LValue) []*engine.Task {
	return append(
		h.completeTask(senderTask, lua.LBool(true)),
		h.completeTask(receiverTask, value)...,
	)
}

func (h *OperationHandler) completeSelectCase(task *engine.Task, selectCase *selectSwitch, ch *Channel, value lua.LValue, ok bool) []*engine.Task {
	task.Resumed = []lua.LValue{selectCase.caseResult(task.Thread(), ch, value, ok)}
	return []*engine.Task{task}
}

func (h *OperationHandler) registerSelectCases(task *engine.Task, op *selectSwitch) []*engine.Task {
	for _, sc := range op.cases {
		if ch := sc.Channel(); ch != nil {
			h.queueSelectCase(task, ch, sc, op)
		}
	}

	return nil
}

func (h *OperationHandler) queueSelectCase(task *engine.Task, ch *Channel, sc *selectCase, selectData *selectSwitch) {
	pOp := &pendingOp{
		task: task,
		op: &chanOperation{
			opType: sc.dir,
			ch:     ch,
			value:  sc.value,
		},
		selectOp: selectData,
	}

	if ch.IsNamed() {
		h.inbox.addReceiver(ch.Name(), pOp)
	} else if sc.dir == chanSend {
		h.senders.enqueue(ch, pOp)
	} else {
		h.receivers.enqueue(ch, pOp)
	}
}
