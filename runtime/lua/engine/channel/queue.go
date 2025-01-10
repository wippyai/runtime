package channel

import (
	"sync"
)

// Pool of reusable objects to reduce allocations
var (
	pendingPool = sync.Pool{New: func() interface{} { return &pendingOp{} }}
	queuePool   = sync.Pool{New: func() interface{} { return &pendingQueue{} }}
)

// pendingQueue is not thread safe, external synchronization is required
type pendingQueue struct {
	head  *pendingOp
	tail  *pendingOp
	named map[string][]*pendingOp
	size  int
}

func (q *pendingQueue) enqueue(op *pendingOp) {
	if q.size == 0 {
		q.head = op
		q.tail = op
	} else {
		q.tail.next = op
		q.tail = op
	}
	q.size++
}

func (q *pendingQueue) dequeue() *pendingOp {
	if q.size == 0 {
		return nil
	}

	op := q.head
	q.head = op.next
	op.next = nil // Avoid potential memory leak
	q.size--

	if q.size == 0 {
		q.tail = nil
	}

	return op
}

// remove removes a specific node from the queue
// Returns true if node was found and removed, false otherwise
func (q *pendingQueue) remove(op *pendingOp) bool {
	if q.size == 0 {
		return false
	}

	if q.head == op {
		q.dequeue()
		return true
	}

	// Walk queue to find predecessor of target node
	for curr := q.head; curr != nil && curr.next != nil; curr = curr.next {
		if curr.next == op {
			curr.next = op.next
			if curr.next == nil {
				q.tail = curr
			}
			q.size--
			return true
		}
	}

	return false
}

func (q *pendingQueue) reset() {
	q.head = nil
	q.tail = nil
	q.size = 0
}

func (q *pendingQueue) clear() {
	current := q.head
	for current != nil {
		next := current.next
		current.reset()
		pendingPool.Put(current)
		current = next
	}
	q.reset()
}

// queueMapper handles mappings between channels and their pending operation queues
type queueMapper struct {
	queues map[*Channel]*pendingQueue
}

func newQueueMapper() *queueMapper {
	return &queueMapper{
		queues: make(map[*Channel]*pendingQueue),
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
	if !exists || queue.size == 0 {
		return nil
	}

	op := queue.dequeue()

	// Clean up empty queue
	if queue.size == 0 {
		delete(m.queues, ch)
		queue.reset()
		queuePool.Put(queue)
	}

	return op
}

// getQueueSize returns size of channel's queue
func (m *queueMapper) getQueueSize(ch *Channel) int {
	if queue, exists := m.queues[ch]; exists {
		return queue.size
	}

	return 0
}

// clear removes all operations from all queues
func (m *queueMapper) clear() {
	for ch, queue := range m.queues {
		queue.clear() // This will return ops to pool
		delete(m.queues, ch)
		queuePool.Put(queue)
	}
}
