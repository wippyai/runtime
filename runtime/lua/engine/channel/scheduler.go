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
	selectOp *selectOperation // If this op is part of a select
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

// scheduler manages all channel operations and state, not thread-safe
type scheduler struct {
	senders   *queueMapper
	receivers *queueMapper
}

// newScheduler creates a new scheduler instance
func newScheduler() *scheduler {
	return &scheduler{
		senders:   newQueueMapper(),
		receivers: newQueueMapper(),
	}
}

// handleChannelTasks processes tasks that contain channel operations
func (c *scheduler) handleChannelTasks(tasks []*engine.Task) ([]*engine.Task, error) {
	var externalTasks []*engine.Task
	var channelTasks []*engine.Task

	for _, task := range tasks {
		if len(task.Yielded) == 0 {
			externalTasks = append(externalTasks, task)
			continue
		}

		var resultTasks []*engine.Task
		switch op := task.Yielded[0].(type) {
		case *chanOperation:
			resultTasks = c.handleOperation(task, op)
		case *selectOperation:
			resultTasks = c.handleSelect(task, op)
		default:
			externalTasks = append(externalTasks, task)
		}

		channelTasks = append(channelTasks, resultTasks...)
	}

	return append(externalTasks, channelTasks...), nil
}

// handleOperation processes a single channel operation
func (c *scheduler) handleOperation(task *engine.Task, op *chanOperation) []*engine.Task {
	switch op.opType {
	case chanSend:
		return c.handleSend(task, op)
	case chanReceive:
		return c.handleReceive(task, op)
	case chanClose:
		return c.handleClose(task, op)
	}

	return nil
}

// getWaitingNames returns list of inbox channel names being listened to. Order is not guaranteed, expect external ordering.
func (c *scheduler) getWaitingNames() []string {
	var names []string
	for name, _ := range c.receivers.named {
		names = append(names, name)
	}

	return names
}

// send sends a value to an inbox channel
func (c *scheduler) send(name string, value lua.LValue) ([]*engine.Task, error) {
	op := c.receivers.dequeueNamed(name)
	if op == nil {
		return nil, fmt.Errorf("no receiver found for channel %s", name)
	}

	if op.selectOp == nil {
		op.task.Resumed = []lua.LValue{value, lua.LBool(true)}
	} else {
		op.task.Resumed = []lua.LValue{op.selectOp.caseResult(
			op.task.Thread(),
			op.op.ch,
			value,
			true,
		)}
	}
	results := []*engine.Task{op.task}

	op.reset()
	pendingPool.Put(op)

	return results, nil

}

// handleSend processes a send operation
func (c *scheduler) handleSend(task *engine.Task, op *chanOperation) []*engine.Task {
	ch := op.ch
	if ch.closed {
		task.Resumed = []lua.LValue{lua.LNil}
		return []*engine.Task{task}
	}

	// Try buffer first for buffered channels
	if ch.capacity > 0 && !ch.isFull() {
		if ch.send(op.value) {
			task.Resumed = []lua.LValue{lua.LBool(true)}
			return []*engine.Task{task}
		}
	}

	// Try matching with a waiting receiver
	if receiver := c.receivers.dequeue(ch); receiver != nil {
		if receiver.selectOp == nil {
			receiver.task.Resumed = []lua.LValue{op.value}
		} else {
			receiver.task.Resumed = []lua.LValue{receiver.selectOp.caseResult(
				receiver.task.Thread(), ch, op.value, true,
			)}
		}

		// Complete both operations
		task.Resumed = []lua.LValue{lua.LBool(true)}
		result := []*engine.Task{task, receiver.task}

		receiver.reset()
		pendingPool.Put(receiver)

		return result
	}

	// Queue the sender
	node := pendingPool.Get().(*pendingOp)
	node.task = task
	node.op = op
	c.senders.enqueue(ch, node)

	return nil
}

// handleReceive processes a receive operation
func (c *scheduler) handleReceive(task *engine.Task, op *chanOperation) []*engine.Task {
	ch := op.ch

	// Try to receive any buffered value first
	if value, ok := ch.receive(); ok {
		task.Resumed = []lua.LValue{value, lua.LBool(true)}
		return []*engine.Task{task}
	}

	if ch.closed {
		task.Resumed = []lua.LValue{lua.LNil, lua.LBool(false)}
		return []*engine.Task{task}
	}

	if !ch.IsNamed() {
		if sender := c.senders.dequeue(ch); sender != nil {
			task.Resumed = []lua.LValue{sender.op.value}

			if sender.selectOp == nil {
				sender.task.Resumed = []lua.LValue{lua.LBool(true)}
			} else {
				sender.task.Resumed = []lua.LValue{sender.selectOp.caseResult(
					sender.task.Thread(), ch, nil, true,
				)}
			}

			result := []*engine.Task{task, sender.task}

			sender.reset()
			pendingPool.Put(sender)

			return result
		}
	}

	// Queue the receiver
	node := pendingPool.Get().(*pendingOp)
	node.task = task
	node.op = op
	c.receivers.enqueue(ch, node)

	return nil
}

// handleClose processes a close operation
func (c *scheduler) handleClose(task *engine.Task, op *chanOperation) []*engine.Task {
	ch := op.ch
	ch.closed = true
	task.Resumed = []lua.LValue{lua.LBool(true)}

	// Count total pending tasks
	total := 1 // for close task
	sendersQueue := c.senders.queues[ch]
	if sendersQueue != nil {
		total += sendersQueue.size()
	}
	receiversQueue := c.receivers.queues[ch]
	if receiversQueue != nil {
		total += receiversQueue.size()
	}

	// Pre-allocate result slice
	result := make([]*engine.Task, 0, total)
	result = append(result, task)

	// Resume all senders with channel closed indicator
	for sender := c.senders.dequeue(ch); sender != nil; sender = c.senders.dequeue(ch) {
		if sender.selectOp == nil {
			sender.task.Resumed = []lua.LValue{lua.LNil} // channel closed
		} else {
			sender.task.RaiseError = fmt.Errorf("channel closed")
		}

		result = append(result, sender.task)
		sender.reset()
		pendingPool.Put(sender)
	}

	// Handle receivers - they can still get buffered values
	for receiver := c.receivers.dequeue(ch); receiver != nil; receiver = c.receivers.dequeue(ch) {
		// Try to receive any buffered value first
		if value, ok := ch.receive(); ok {
			if receiver.selectOp == nil {
				receiver.task.Resumed = []lua.LValue{value, lua.LBool(true)}
			} else {
				receiver.task.Resumed = []lua.LValue{receiver.selectOp.caseResult(
					receiver.task.Thread(), ch, value, true,
				)}
			}
		} else {
			if receiver.selectOp == nil {
				receiver.task.Resumed = []lua.LValue{lua.LNil, lua.LBool(false)} // channel closed
			} else {
				receiver.task.Resumed = []lua.LValue{receiver.selectOp.caseResult(
					receiver.task.Thread(), ch, nil, false,
				)}
			}
		}

		result = append(result, receiver.task)
		receiver.reset()
		pendingPool.Put(receiver)
	}

	return result
}

// handleSelect processes a select operation
func (c *scheduler) handleSelect(task *engine.Task, op *selectOperation) []*engine.Task {
	// Register all cases
	for _, sc := range op.cases {
		ch := sc.Channel()

		pOp := &pendingOp{
			task: task,
			op: &chanOperation{
				opType: sc.dir,
				ch:     ch,
				value:  sc.value,
			},
			selectOp: op,
		}

		if sc.dir == chanSend {
			c.senders.enqueue(ch, pOp)
		} else {
			c.receivers.enqueue(ch, pOp)
		}
		// todo: remove select
	}

	return nil
}

// Cleanup releases all resources and resets state
func (c *scheduler) Cleanup() {
	c.senders.clear()
	c.receivers.clear()
}
