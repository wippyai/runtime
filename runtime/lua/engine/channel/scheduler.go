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
	senders   map[*Channel]*pendingQueue
	receivers map[*Channel]*pendingQueue
	external  *signals
}

func NewScheduler() *Scheduler {
	return &Scheduler{
		senders:   make(map[*Channel]*pendingQueue),
		receivers: make(map[*Channel]*pendingQueue),
		external:  newSignals(),
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
				// should never happen
				externalTasks = append(externalTasks, task)
				continue
			}

			switch op := task.Yielded[0].(type) {
			case *chanOperation:
				channelTasks = append(channelTasks, s.pushOperation(task, op)...)
			case *selectOperation:
				channelTasks = append(channelTasks, s.handleSelect(task, op)...)
			default:
				externalTasks = append(externalTasks, task)
			}
		}

		if len(channelTasks) == 0 {
			break
		}

		// keep going until we done with all channel operations
		vmTasks, err = vm.Step(channelTasks...)
		channelTasks = nil
		if err != nil {
			return nil, fmt.Errorf("coroutine failed: %w", err)
		}
	}

	return externalTasks, nil
}

func (s *Scheduler) handleSelect(task *engine.Task, op *selectOperation) []*engine.Task {
	selectData := &selectData{
		parentSelect: op,
		channels:     make([]*selectCase, 0, len(op.cases)),
	}

	// Register all cases
	for _, sc := range op.cases {
		ch := sc.Channel()
		if ch == nil {
			continue
		}

		// Track case for later result creation
		selectData.channels = append(selectData.channels, sc)

		// Create pending operation
		pOp := &pendingOp{
			task: task,
			op: &chanOperation{
				opType: sc.dir,
				ch:     ch,
				value:  sc.value,
			},
			selectCase: selectData,
		}

		if ch.IsExternal() {
			s.external.addReceiver(ch.ExternalName(), pOp) // not queued since handled from outside
			continue
		}

		// Queue based on operation type
		if sc.dir == chanSend {
			s.enqueue(s.senders, ch, pOp)
		} else {
			s.enqueue(s.receivers, ch, pOp)
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

func (s *Scheduler) enqueue(m map[*Channel]*pendingQueue, ch *Channel, node *pendingOp) {
	queue, exists := m[ch]
	if !exists || queue == nil {
		queue = queuePool.Get().(*pendingQueue)
		queue.reset()
		queue.head = node
		queue.tail = node
		m[ch] = queue
		return
	}
	queue.tail.next = node
	queue.tail = node
}

func (s *Scheduler) dequeue(m map[*Channel]*pendingQueue, ch *Channel) *pendingOp {
	queue, exists := m[ch]
	if !exists || queue == nil || queue.head == nil {
		return nil
	}

	node := queue.head
	queue.head = node.next
	node.next = nil

	if queue.head == nil {
		queue.tail = nil
		delete(m, ch)
		queue.reset()
		queuePool.Put(queue)
	}

	return node
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

	if receiver := s.dequeue(s.receivers, ch); receiver != nil {
		if receiver.selectCase == nil {
			receiver.task.Resumed = []lua.LValue{op.value}
		} else {
			receiver.task.Resumed = []lua.LValue{receiver.selectCase.makeCaseResult(
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
	s.enqueue(s.senders, ch, node)

	return nil
}

func (s *Scheduler) handleReceive(task *engine.Task, op *chanOperation) []*engine.Task {
	ch := op.ch

	// Try to receive from buffer first
	if value, ok := ch.receive(); ok {
		task.Resumed = []lua.LValue{value, lua.LBool(true)}
		return []*engine.Task{task}
	}

	if ch.closed {
		task.Resumed = []lua.LValue{lua.LNil, lua.LBool(false)}
		return []*engine.Task{task}
	}

	// Check for waiting sender
	if sender := s.dequeue(s.senders, ch); sender != nil {
		task.Resumed = []lua.LValue{sender.op.value}

		if sender.selectCase == nil {
			sender.task.Resumed = []lua.LValue{lua.LBool(true)}
		} else {
			sender.task.Resumed = []lua.LValue{sender.selectCase.makeCaseResult(
				sender.task.Thread(), ch, nil, true,
			)}
		}

		result := []*engine.Task{task, sender.task}

		sender.reset()
		pendingPool.Put(sender)

		return result
	}

	if ch.IsExternal() {
		// Create pending op
		node := pendingPool.Get().(*pendingOp)
		node.task = task
		node.op = op

		// not queued since handled from outside
		s.external.addReceiver(ch.ExternalName(), node)
		return nil
	}

	// Queue the receiver
	node := pendingPool.Get().(*pendingOp)
	node.task = task
	node.op = op
	s.enqueue(s.receivers, ch, node)

	return nil
}

func (s *Scheduler) handleClose(task *engine.Task, op *chanOperation) []*engine.Task {
	ch := op.ch
	ch.closed = true
	task.Resumed = []lua.LValue{lua.LBool(true)}

	// Count total pending tasks
	total := 1 // for close task
	if queue := s.senders[ch]; queue != nil {
		for p := queue.head; p != nil; p = p.next {
			total++
		}
	}
	if queue := s.receivers[ch]; queue != nil {
		for p := queue.head; p != nil; p = p.next {
			total++
		}
	}

	// Pre-allocate result slice
	result := make([]*engine.Task, 0, total)
	result = append(result, task)

	// resume all senders with channel closed indicator
	for sender := s.dequeue(s.senders, ch); sender != nil; sender = s.dequeue(s.senders, ch) {
		if sender.selectCase == nil {
			sender.task.Resumed = []lua.LValue{lua.LNil} // channel closed
		} else {
			sender.task.Error = fmt.Errorf("channel closed")
		}

		result = append(result, sender.task)
		sender.reset()
		pendingPool.Put(sender)
	}

	// handle receivers - they can still get buffered values
	for receiver := s.dequeue(s.receivers, ch); receiver != nil; receiver = s.dequeue(s.receivers, ch) {
		// Try to receive any buffered value first
		if value, ok := ch.receive(); ok {
			if receiver.selectCase == nil {
				receiver.task.Resumed = []lua.LValue{value, lua.LBool(true)}
			} else {
				receiver.task.Resumed = []lua.LValue{receiver.selectCase.makeCaseResult(
					receiver.task.Thread(), ch, value, true,
				)}
			}
		} else {
			if receiver.selectCase == nil {
				receiver.task.Resumed = []lua.LValue{lua.LNil, lua.LBool(false)} // channel closed
			} else {
				receiver.task.Resumed = []lua.LValue{receiver.selectCase.makeCaseResult(
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

// ActiveSignals returns a list of external channel names currently being listened to
func (s *Scheduler) ActiveSignals() []string {
	return s.external.getNames()
}

// Signal sends a value to an external channel
func (s *Scheduler) Signal(name string, value lua.LValue) []*engine.Task {
	if op := s.external.popReceiver(name); op != nil {
		// If this was part of a select, clean up other external channels
		if op.selectCase != nil {
			for _, sc := range op.selectCase.channels {
				ch := sc.Channel()
				if ch != nil && ch.IsExternal() && ch.ExternalName() != name {
					s.external.removeReceiver(ch.ExternalName(), op)
				}
			}
		}

		if op.selectCase == nil {
			op.task.Resumed = []lua.LValue{value, lua.LBool(true)}
		} else {
			op.task.Resumed = []lua.LValue{op.selectCase.makeCaseResult(
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
