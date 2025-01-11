package channel

import (
	"container/list"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"sync"
)

// Pool of reusable objects to reduce allocations
var (
	pendingPool = sync.Pool{New: func() interface{} { return &pendingOp{} }}
	queuePool   = sync.Pool{New: func() interface{} { return &pendingQueue{} }}
)

// pendingQueue is not thread safe, external synchronization is required
type pendingQueue struct {
	ops       *list.List
	named     map[string][]*pendingOp
	selectOps map[*selectOperation][]*list.Element // Track elements for each select
}

// Add these functions at the top level in queue.go after the pool declarations:

// newPendingOp creates a new pendingOp with the given parameters
func newPendingOp(task *engine.Task, op *chanOperation, selectOp *selectOperation) *pendingOp {
	pending := pendingPool.Get().(*pendingOp)
	pending.task = task
	pending.op = op
	pending.selectOp = selectOp
	return pending
}

// releasePendingOp returns a pendingOp to the pool
func releasePendingOp(op *pendingOp) {
	if op != nil {
		op.reset()
		pendingPool.Put(op)
	}
}

// newPendingQueueFromPool creates a new pendingQueue from the pool
func newPendingQueueFromPool() *pendingQueue {
	queue := queuePool.Get().(*pendingQueue)
	queue.reset()
	return queue
}

// releasePendingQueue returns a pendingQueue to the pool
func releasePendingQueue(queue *pendingQueue) {
	if queue != nil {
		queue.clear() // This already calls reset
		queuePool.Put(queue)
	}
}

func newPendingQueue() *pendingQueue {
	return &pendingQueue{
		ops:       list.New(),
		named:     make(map[string][]*pendingOp),
		selectOps: make(map[*selectOperation][]*list.Element),
	}
}

func (q *pendingQueue) reset() {
	if q.ops != nil {
		q.ops.Init()
	} else {
		q.ops = list.New()
	}
	q.named = make(map[string][]*pendingOp)
	q.selectOps = make(map[*selectOperation][]*list.Element)
}

func (q *pendingQueue) enqueue(op *pendingOp) {
	elem := q.ops.PushBack(op)

	// If this is part of a select, track its element
	if op.selectOp != nil {
		q.selectOps[op.selectOp] = append(q.selectOps[op.selectOp], elem)
	}
}

func (q *pendingQueue) dequeue() *pendingOp {
	if q.ops.Len() == 0 {
		return nil
	}

	elem := q.ops.Front()
	op := elem.Value.(*pendingOp)

	// If it was part of a select, remove from tracking
	if op.selectOp != nil {
		delete(q.selectOps, op.selectOp)
	}

	q.ops.Remove(elem)
	return op
}

// removeSelect removes all operations that belong to the same select operation
func (q *pendingQueue) removeSelect(selectOp *selectOperation) {
	if selectOp == nil {
		return
	}

	// Use our tracked elements to directly remove the ops
	if elements, exists := q.selectOps[selectOp]; exists {
		for _, elem := range elements {
			if elem != nil {
				op := elem.Value.(*pendingOp)
				q.ops.Remove(elem)
				op.reset()
				pendingPool.Put(op)
			}
		}
		delete(q.selectOps, selectOp)
	}
}

func (q *pendingQueue) remove(op *pendingOp) bool {
	if op == nil {
		return false
	}

	// Find and remove the operation
	for e := q.ops.Front(); e != nil; e = e.Next() {
		if e.Value.(*pendingOp) == op {
			// If it was part of a select, clean up tracking
			if op.selectOp != nil {
				delete(q.selectOps, op.selectOp)
			}
			q.ops.Remove(e)
			return true
		}
	}
	return false
}

func (q *pendingQueue) size() int {
	return q.ops.Len()
}

func (q *pendingQueue) clear() {
	for e := q.ops.Front(); e != nil; e = e.Next() {
		op := e.Value.(*pendingOp)
		op.reset()
		pendingPool.Put(op)
	}
	q.reset()
}

// queueMapper handles mappings between channels and their pending operation queues
type queueMapper struct {
	queues map[*Channel]*pendingQueue
	named  map[string]*pendingQueue
}

func newQueueMapper() *queueMapper {
	return &queueMapper{
		queues: make(map[*Channel]*pendingQueue),
		named:  make(map[string]*pendingQueue),
	}
}

// allocateQueue returns existing queue or creates new one if doesn't exist
func (m *queueMapper) allocateQueue(ch *Channel) *pendingQueue {
	if queue, exists := m.queues[ch]; exists {
		return queue
	}

	queue := queuePool.Get().(*pendingQueue)
	queue.reset()
	m.queues[ch] = queue

	if ch.name != "" {
		m.named[ch.name] = queue // alias
	}

	return queue
}

// enqueue adds an operation to channel's queue
func (m *queueMapper) enqueue(ch *Channel, op *pendingOp) {
	queue := m.allocateQueue(ch)
	queue.enqueue(op)
}

// dequeue removes and returns first operation from channel's queue
func (m *queueMapper) dequeue(ch *Channel) *pendingOp {
	queue, exists := m.queues[ch]
	if !exists {
		return nil
	}

	op := queue.dequeue()
	if op == nil {
		return nil
	}

	if queue.ops.Len() == 0 {
		delete(m.queues, ch)
		if ch.name != "" {
			delete(m.named, ch.name)
		}
		queue.reset()
		queuePool.Put(queue)
	}

	return op
}

func (m *queueMapper) dequeueNamed(name string) *pendingOp {
	queue, exists := m.named[name]
	if !exists {
		return nil
	}

	op := queue.dequeue()
	if op == nil {
		return nil
	}

	// Clean up empty queue
	if queue.size() == 0 {
		delete(m.named, name)
		delete(m.queues, op.op.ch)
		queue.reset()
		queuePool.Put(queue)
	}

	return op
}

// getQueueSize returns size of channel's queue
func (m *queueMapper) getQueueSize(ch *Channel) int {
	if queue, exists := m.queues[ch]; exists {
		return queue.size()
	}
	return 0
}

func (m *queueMapper) getNamedQueueSize(name string) int {
	if queue, exists := m.named[name]; exists {
		return queue.size()
	}
	return 0
}

// removeSelect removes all operations belonging to the given select from all queues
func (m *queueMapper) removeSelect(selectOp *selectOperation) {
	for ch, queue := range m.queues {
		queue.removeSelect(selectOp)
		// If queue is empty after removal, clean it up
		if queue.size() == 0 {
			delete(m.queues, ch)
			if ch.name != "" {
				delete(m.named, ch.name)
			}
			queue.reset()
			queuePool.Put(queue)
		}
	}
}

// clear removes all operations from all queues
func (m *queueMapper) clear() {
	for ch, queue := range m.queues {
		queue.clear()
		delete(m.queues, ch)
		queuePool.Put(queue)
	}
}
