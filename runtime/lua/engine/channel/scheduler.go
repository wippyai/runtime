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

// carries chan operation context over to the bufferedScheduler
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
	dir   chanOp
	ch    *Channel
	value lua.LValue
}

func (y *chanOperation) String() string {
	switch y.dir {
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

// bufferedScheduler manages all channel operations and state, not thread-safe
type bufferedScheduler struct {
	senders   *queueMapper
	receivers *queueMapper
}

// newScheduler creates a new bufferedScheduler instance
func newScheduler() *bufferedScheduler {
	return &bufferedScheduler{
		senders:   newQueueMapper(),
		receivers: newQueueMapper(),
	}
}

// handleTasks processes tasks that contain channel operations
func (s *bufferedScheduler) handleTasks(tasks []*engine.Task) ([]*engine.Task, error) {
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
			resultTasks = s.handleOperation(task, op)
		case *selectOperation:
			resultTasks = s.scheduleSelect(task, op)
			if len(resultTasks) > 0 {
				s.senders.removeSelect(op)
				s.receivers.removeSelect(op)
			}
		default:
			externalTasks = append(externalTasks, task)
		}

		channelTasks = append(channelTasks, resultTasks...)
	}

	return append(externalTasks, channelTasks...), nil
}

// handleOperation processes a single channel operation
func (s *bufferedScheduler) handleOperation(task *engine.Task, op *chanOperation) []*engine.Task {
	switch op.dir {
	case chanSend:
		fmt.Printf("Scheduler: Handling send on channel %p\n", op.ch)
		return s.handleSend(task, op)
	case chanReceive:
		fmt.Printf("Scheduler: Handling receive on channel %p\n", op.ch)
		return s.handleReceive(task, op)
	case chanClose:
		return s.handleClose(task, op)
	}

	return nil
}

// getOpenChannels returns list of inbox channel names being listened to. Order is not guaranteed, expect external ordering.
func (s *bufferedScheduler) getOpenChannels() []string {
	var names []string
	for name, _ := range s.receivers.named {
		names = append(names, name)
	}

	return names
}

// send sends a value to an inbox channel
func (s *bufferedScheduler) send(name string, value lua.LValue) ([]*engine.Task, error) {
	op := s.receivers.dequeueNamed(name)
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
func (s *bufferedScheduler) handleSend(task *engine.Task, op *chanOperation) []*engine.Task {
	ch := op.ch
	if ch.closed {
		task.Resumed = []lua.LValue{lua.LNil}
		return []*engine.Task{task}
	}

	// Try matching with a waiting receiver
	if receiver := s.receivers.dequeue(ch); receiver != nil {
		if receiver.selectOp == nil {
			receiver.task.Resumed = []lua.LValue{op.value}
		} else {
			receiver.task.Resumed = []lua.LValue{receiver.selectOp.caseResult(
				receiver.task.Thread(), ch, op.value, true,
			)}
			s.cleanupSelectOperation(receiver.selectOp)
		}

		// Complete both operations
		task.Resumed = []lua.LValue{lua.LBool(true)}
		result := []*engine.Task{task, receiver.task}

		receiver.reset()
		pendingPool.Put(receiver)

		return result
	}

	// Try buffer first for buffered channels
	if ch.capacity > 0 && !ch.isFull() {
		if ch.send(op.value) {
			task.Resumed = []lua.LValue{lua.LBool(true)}
			return []*engine.Task{task}
		}
	}

	// Queue the sender
	node := pendingPool.Get().(*pendingOp)
	node.task = task
	node.op = op
	s.senders.enqueue(ch, node)

	return nil // no tasks, blocked
}

// handleReceive processes a receive operation
func (s *bufferedScheduler) handleReceive(task *engine.Task, op *chanOperation) []*engine.Task {
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
		if sender := s.senders.dequeue(ch); sender != nil {
			task.Resumed = []lua.LValue{sender.op.value}

			if sender.selectOp == nil {
				sender.task.Resumed = []lua.LValue{lua.LBool(true)}
			} else {
				sender.task.Resumed = []lua.LValue{sender.selectOp.caseResult(
					sender.task.Thread(), ch, nil, true,
				)}
				s.cleanupSelectOperation(sender.selectOp)
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
	s.receivers.enqueue(ch, node)

	return nil
}

// handleClose processes a close operation
func (s *bufferedScheduler) handleClose(task *engine.Task, op *chanOperation) []*engine.Task {
	ch := op.ch
	if ch.closed {
		task.RaiseError = fmt.Errorf("channel already closed")
		return []*engine.Task{task}
	}

	ch.closed = true
	task.Resumed = []lua.LValue{lua.LBool(true)}

	// Count total pending tasks
	total := 1 // for close task
	sendersQueue := s.senders.queues[ch]
	if sendersQueue != nil {
		total += sendersQueue.size()
	}
	receiversQueue := s.receivers.queues[ch]
	if receiversQueue != nil {
		total += receiversQueue.size()
	}

	// Pre-allocate result slice
	result := make([]*engine.Task, 0, total)
	result = append(result, task)

	// Resume all senders with channel closed indicator
	for sender := s.senders.dequeue(ch); sender != nil; sender = s.senders.dequeue(ch) {
		sender.task.RaiseError = fmt.Errorf("channel closed")

		s.cleanupSelectOperation(sender.selectOp)

		result = append(result, sender.task)
		sender.reset()
		pendingPool.Put(sender)
	}

	// Handle receivers - they can still get buffered values, others will read during receive calls
	for receiver := s.receivers.dequeue(ch); receiver != nil; receiver = s.receivers.dequeue(ch) {
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

		s.cleanupSelectOperation(receiver.selectOp)

		result = append(result, receiver.task)
		receiver.reset()
		pendingPool.Put(receiver)
	}

	return result
}

// scheduleSelect processes a select operation
func (s *bufferedScheduler) scheduleSelect(task *engine.Task, op *selectOperation) []*engine.Task {
	// Register all cases
	for _, sc := range op.cases {
		ch := sc.Channel()

		pOp := &pendingOp{
			task:     task,
			op:       &chanOperation{dir: sc.dir, ch: ch, value: sc.value},
			selectOp: op,
		}

		if sc.dir == chanSend {
			s.senders.enqueue(ch, pOp)
		} else {
			s.receivers.enqueue(ch, pOp)
		}
	}

	return nil
}

// cleanupSelectOperation removes all traces of a select operation from both queue types
func (s *bufferedScheduler) cleanupSelectOperation(selectOp *selectOperation) {
	if selectOp == nil {
		return
	}
	s.senders.removeSelect(selectOp)
	s.receivers.removeSelect(selectOp)
}

// Cleanup releases all resources and resets state
func (s *bufferedScheduler) close() {
	s.senders.clear()
	s.receivers.clear()
}
