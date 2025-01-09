package channel

import (
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
)

type VM interface {
	Step(tasks ...*engine.Task) ([]*engine.Task, error)
}

type Scheduler struct {
	senders   *queueMapper
	receivers *queueMapper
	inbox     *inbox
}

func NewScheduler() *Scheduler {
	return &Scheduler{
		senders:   newQueueMapper(),
		receivers: newQueueMapper(),
		inbox:     newInbox(),
	}
}

func (s *Scheduler) Step(vm VM, tasks ...*engine.Task) ([]*engine.Task, error) {
	vmTasks, err := vm.Step(tasks...)
	if err != nil {
		return nil, err
	}

	var externalTasks []*engine.Task
	var channelTasks []*engine.Task

	// Keep processing until all channel operations are handled
	for len(vmTasks) > 0 {
		for _, task := range vmTasks {
			if len(task.Yielded) == 0 {
				externalTasks = append(externalTasks, task)
				continue
			}

			switch op := task.Yielded[0].(type) {
			case *chanOperation:
				channelTasks = append(channelTasks, s.pushOperation(task, op)...)
			case *selectSwitch:
				channelTasks = append(channelTasks, s.handleSelect(task, op)...)
			default:
				externalTasks = append(externalTasks, task)
			}
		}

		if len(channelTasks) == 0 {
			break
		}

		// Keep going until we're done with all channel operations
		vmTasks, err = vm.Step(channelTasks...)
		channelTasks = nil
		if err != nil {
			return nil, fmt.Errorf("coroutine failed: %w", err)
		}
	}

	return externalTasks, nil
}

func (s *Scheduler) handleSelect(task *engine.Task, op *selectSwitch) []*engine.Task {
	// Register all cases
	for _, sc := range op.cases {
		ch := sc.Channel()
		if ch == nil {
			continue
		}

		// Create pending operation
		pOp := &pendingOp{
			task: task,
			op: &chanOperation{
				opType: sc.dir,
				ch:     ch,
				value:  sc.value,
			},
			selectOp: op,
		}

		if ch.IsNamed() {
			s.inbox.addReceiver(ch.Name(), pOp)
			continue
		}

		// Queue based on operation type
		if sc.dir == chanSend {
			s.senders.enqueue(ch, pOp)
		} else {
			s.receivers.enqueue(ch, pOp)
		}
	}

	return nil
}

func (s *Scheduler) pushOperation(task *engine.Task, op *chanOperation) []*engine.Task {
	switch op.opType {
	case chanSend:
		return s.handleSend(task, op)
	case chanReceive:
		return s.handleReceive(task, op)
	case chanClose:
		return s.handleClose(task, op)
	}

	return nil
}

func (s *Scheduler) handleSend(task *engine.Task, op *chanOperation) []*engine.Task {
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

	if receiver := s.receivers.dequeue(ch); receiver != nil {
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
	s.senders.enqueue(ch, node)

	return nil
}

func (s *Scheduler) handleReceive(task *engine.Task, op *chanOperation) []*engine.Task {
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

	// Check for waiting sender
	if sender := s.senders.dequeue(ch); sender != nil {
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

	if ch.IsNamed() {
		// Create pending op
		node := pendingPool.Get().(*pendingOp)
		node.task = task
		node.op = op

		// Not queued since handled from outside
		s.inbox.addReceiver(ch.Name(), node)
		return nil
	}

	// Queue the receiver
	node := pendingPool.Get().(*pendingOp)
	node.task = task
	node.op = op
	s.receivers.enqueue(ch, node)

	return nil
}

func (s *Scheduler) handleClose(task *engine.Task, op *chanOperation) []*engine.Task {
	ch := op.ch
	ch.closed = true
	task.Resumed = []lua.LValue{lua.LBool(true)}

	// Count total pending tasks
	total := 1 // for close task
	sendersQueue := s.senders.queues[ch]
	if sendersQueue != nil {
		total += sendersQueue.size
	}
	receiversQueue := s.receivers.queues[ch]
	if receiversQueue != nil {
		total += receiversQueue.size
	}

	// Pre-allocate result slice
	result := make([]*engine.Task, 0, total)
	result = append(result, task)

	// Resume all senders with channel closed indicator
	for sender := s.senders.dequeue(ch); sender != nil; sender = s.senders.dequeue(ch) {
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

		result = append(result, receiver.task)
		receiver.reset()
		pendingPool.Put(receiver)
	}

	return result
}

// ActiveSignals returns a list of inbox channel names currently being listened to
func (s *Scheduler) ActiveSignals() []string {
	return s.inbox.getNames()
}

// Send sends a value to an inbox channel
func (s *Scheduler) Send(name string, value lua.LValue) []*engine.Task {
	if op := s.inbox.popReceiver(name); op != nil {
		// If this was part of a select, clean up other inbox channels
		if op.selectOp != nil {
			for _, sc := range op.selectOp.cases {
				ch := sc.Channel()
				if ch != nil && ch.IsNamed() && ch.Name() != name {
					s.inbox.removeReceiver(ch.Name(), op)
				}
			}
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

		return results
	}
	return nil
}
