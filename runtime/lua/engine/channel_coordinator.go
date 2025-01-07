package engine

import (
	lua "github.com/yuin/gopher-lua"
	"log"
	"sync"
)

// Pre-allocated results for common cases
var (
	monoResult  = []*Task{nil}
	tupleResult = []*Task{nil, nil}
	noTasks     = make([]*Task, 0)
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
	task *Task
	op   *ChanOperation
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

type ChannelCoordinator struct {
	senders   map[*Channel]*pendingQueue
	receivers map[*Channel]*pendingQueue
}

func NewChannelCoordinator() *ChannelCoordinator {
	return &ChannelCoordinator{
		senders:   make(map[*Channel]*pendingQueue),
		receivers: make(map[*Channel]*pendingQueue),
	}
}

func (cc *ChannelCoordinator) PushOperation(task *Task, op *ChanOperation) []*Task {
	switch op.opType {
	case chanOpSend:
		return cc.handleSend(task, op)
	case chanOpReceive:
		return cc.handleReceive(task, op)
	case chanOpClose:
		return cc.handleClose(task, op)
	}
	return noTasks
}

func (cc *ChannelCoordinator) enqueueOp(m map[*Channel]*pendingQueue, ch *Channel, node *pendingOp) {
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

func (cc *ChannelCoordinator) dequeueOp(m map[*Channel]*pendingQueue, ch *Channel) *pendingOp {
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

func (cc *ChannelCoordinator) handleSend(task *Task, op *ChanOperation) []*Task {
	//.Printf("DEBUG: handleSend starting - task=%p thread=%p op=%+v", task, task.thread, op)
	ch := op.ch
	if ch.closed {
		task.resumeVal = lua.LNil
		monoResult[0] = task
		return monoResult
	}

	// Try buffer first for buffered channels
	if ch.capacity > 0 && !ch.isFull() {
		if ch.send(op.value) {
			task.resumeVal = lua.LBool(true)
			monoResult[0] = task
			//		log.Printf("DEBUG: handleSend returning tasks: %+v", monoResult)
			return monoResult
		}
	}

	// Check for waiting receiver
	//for{
	//	node := cc.dequeueOp(cc.receivers, ch)
	//	if node == nil {
	//		break
	//	}
	//}
	if node := cc.dequeueOp(cc.receivers, ch); node != nil {
		// Complete both operations
		node.task.resumeVal = op.value
		task.resumeVal = lua.LBool(true)

		log.Printf("!!!!DEBUG: handleSend completing rendezvous - sender_task=%p receiver_task=%p", task, node.task)
		tupleResult[0] = node.task
		tupleResult[1] = task

		node.reset()
		pendingPool.Put(node)

		//	log.Printf("DEBUG: handleSend returning tasks: %+v", tupleResult)
		return tupleResult
	}

	// Queue the sender
	node := pendingPool.Get().(*pendingOp)
	node.task = task
	node.op = op
	cc.enqueueOp(cc.senders, ch, node)
	//	log.Printf("DEBUG: handleSend returning tasks: %+v", noTasks)

	return noTasks
}

func (cc *ChannelCoordinator) handleReceive(task *Task, op *ChanOperation) []*Task {
	//	log.Printf("DEBUG: handleReceive starting - task=%p thread=%p op=%+v", task, task.thread, op)

	ch := op.ch

	// Try to receive from buffer first
	if value, ok := ch.receive(); ok {
		task.resumeVal = value
		monoResult[0] = task
		log.Printf("DEBUG: handleReceive returning tasks: %+v", monoResult)
		// todo: logic here is not valid, we need to unblock sender as well if needed
		return monoResult
	}

	if ch.closed {
		task.resumeVal = lua.LNil
		monoResult[0] = task
		log.Printf("DEBUG: handleReceive returning tasks: %+v", monoResult)
		return monoResult
	}

	// Check for waiting sender
	if node := cc.dequeueOp(cc.senders, ch); node != nil {
		// Complete both operations
		task.resumeVal = node.op.value
		node.task.resumeVal = lua.LBool(true)

		tupleResult[0] = task
		tupleResult[1] = node.task

		node.reset()
		pendingPool.Put(node)

		log.Printf("DEBUG: handleReceive completing rendezvous - receiver_task=%p sender_task=%p", task, node.task)
		return tupleResult
	}

	// Queue the receiver
	node := pendingPool.Get().(*pendingOp)
	node.task = task
	node.op = op
	cc.enqueueOp(cc.receivers, ch, node)
	//log.Printf("DEBUG: handleReceive returning tasks: %+v", noTasks)

	return noTasks
}

func (cc *ChannelCoordinator) handleClose(task *Task, op *ChanOperation) []*Task {
	ch := op.ch
	ch.closed = true

	// Count total pending tasks
	total := 1 // for close task
	if queue := cc.senders[ch]; queue != nil {
		for p := queue.head; p != nil; p = p.next {
			total++
		}
	}
	if queue := cc.receivers[ch]; queue != nil {
		for p := queue.head; p != nil; p = p.next {
			total++
		}
	}

	// Pre-allocate result slice
	result := make([]*Task, 0, total)
	result = append(result, task)

	// Resume all senders with channel closed indicator
	for node := cc.dequeueOp(cc.senders, ch); node != nil; node = cc.dequeueOp(cc.senders, ch) {
		node.task.resumeVal = lua.LNil
		result = append(result, node.task)

		// Recycle pending op
		node.reset()
		pendingPool.Put(node)
	}

	// Resume all receivers with channel closed indicator
	for node := cc.dequeueOp(cc.receivers, ch); node != nil; node = cc.dequeueOp(cc.receivers, ch) {
		node.task.resumeVal = lua.LNil
		result = append(result, node.task)

		// Recycle pending op
		node.reset()
		pendingPool.Put(node)
	}

	ch.cleanup()

	return result
}
