package channel

import (
	"github.com/ponyruntime/pony/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
	"sync"
)

// Pool of reusable objects to reduce allocations
var (
	pendingPool = sync.Pool{
		New: func() interface{} { return &pendingOp{} },
	}
	queuePool = sync.Pool{
		New: func() interface{} { return &pendingQueue{} },
	}
)

type pendingOp struct {
	task *engine.Task
	op   *chanOperation
	next *pendingOp
}

func (p *pendingOp) reset() {
	p.task = nil
	p.op = nil
	p.next = nil
}

type pendingQueue struct {
	head *pendingOp
	tail *pendingOp
}

func (q *pendingQueue) reset() {
	q.head = nil
	q.tail = nil
}

type Scheduler struct {
	senders   map[*Channel]*pendingQueue
	receivers map[*Channel]*pendingQueue
}

func NewScheduler() *Scheduler {
	return &Scheduler{
		senders:   make(map[*Channel]*pendingQueue),
		receivers: make(map[*Channel]*pendingQueue),
	}
}

func (s *Scheduler) Step(vm engine.CoroutineVM, task ...*engine.Task) ([]*engine.Task, error) {
	tasks, err := vm.Step(task...)
	if err != nil {
		return nil, err
	}

	var finalTasks []*engine.Task
	for {
		var newTasks []*engine.Task
		for _, t := range tasks {
			if op, ok := t.GetYieldedValues()[0].(*chanOperation); ok {
				newTasks = append(newTasks, s.pushOperation(t, op)...)
			} else {
				finalTasks = append(finalTasks, t)
			}
		}

		if len(newTasks) == 0 {
			break
		}
	}

	return tasks, nil
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

func (s *Scheduler) enqueueOp(m map[*Channel]*pendingQueue, ch *Channel, node *pendingOp) {
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

func (s *Scheduler) dequeueOp(m map[*Channel]*pendingQueue, ch *Channel) *pendingOp {
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
		task.SetResumeValues(lua.LNil)
		return []*engine.Task{task}
	}

	// Try buffer first for buffered channels
	if ch.capacity > 0 && !ch.isFull() {
		if ch.send(op.value) {
			task.SetResumeValues(lua.LBool(true))
			return []*engine.Task{task}
		}
	}
	if node := s.dequeueOp(s.receivers, ch); node != nil {
		// Complete both operations
		node.task.SetResumeValues(op.value)
		task.SetResumeValues(lua.LBool(true))

		result := []*engine.Task{task, node.task}

		node.reset()
		pendingPool.Put(node)

		return result
	}

	// Queue the sender
	node := pendingPool.Get().(*pendingOp)
	node.task = task
	node.op = op
	s.enqueueOp(s.senders, ch, node)

	return nil
}

func (s *Scheduler) handleReceive(task *engine.Task, op *chanOperation) []*engine.Task {
	ch := op.ch

	// Try to receive from buffer first
	if value, ok := ch.receive(); ok {
		task.SetResumeValues(value)
		return []*engine.Task{task}
	}

	if ch.closed {
		task.SetResumeValues(lua.LNil)
		return []*engine.Task{task}
	}

	// Check for waiting sender
	if node := s.dequeueOp(s.senders, ch); node != nil {
		// Complete both operations
		task.SetResumeValues(node.op.value)
		node.task.SetResumeValues(lua.LBool(true))

		result := []*engine.Task{task, node.task}

		node.reset()
		pendingPool.Put(node)

		return result
	}

	// Queue the receiver
	node := pendingPool.Get().(*pendingOp)
	node.task = task
	node.op = op
	s.enqueueOp(s.receivers, ch, node)

	return nil
}

func (s *Scheduler) handleClose(task *engine.Task, op *chanOperation) []*engine.Task {
	ch := op.ch
	ch.closed = true

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

	// Resume all senders with channel closed indicator
	for node := s.dequeueOp(s.senders, ch); node != nil; node = s.dequeueOp(s.senders, ch) {
		node.task.SetResumeValues(lua.LNil)
		result = append(result, node.task)
		node.reset()
		pendingPool.Put(node)
	}

	// Handle receivers - they can still get buffered values
	for node := s.dequeueOp(s.receivers, ch); node != nil; node = s.dequeueOp(s.receivers, ch) {
		// Try to receive any buffered value first
		if value, ok := ch.receive(); ok {
			node.task.SetResumeValues(value)
		} else {
			node.task.SetResumeValues(lua.LNil, lua.LBool(false)) // channel closed
		}
		result = append(result, node.task)
		node.reset()
		pendingPool.Put(node)
	}

	// Only cleanup if no buffered messages remain
	if ch.isEmpty() {
		ch.cleanup()
	}

	return result
}
