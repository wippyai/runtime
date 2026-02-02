package actor

import (
	"sync"
	"sync/atomic"
)

// InjectQueue is a lock-free MPSC (multi-producer, single-consumer) queue.
// Used for async completions to inject processors back to their affine worker.
//
// Based on Vyukov's MPSC queue algorithm:
//   - Producers (async handlers) atomically swap head, then link
//   - Consumer (worker) reads from tail.next
//   - Stub node simplifies empty queue handling
//
// Memory: nodes are pooled to avoid allocation per push.
type InjectQueue struct {
	head atomic.Pointer[injectNode]
	tail *injectNode
	stub injectNode
}

type injectNode struct {
	next atomic.Pointer[injectNode]
	proc *Processor
}

var injectNodePool = sync.Pool{
	New: func() any { return &injectNode{} },
}

func NewInjectQueue() *InjectQueue {
	q := &InjectQueue{}
	q.head.Store(&q.stub)
	q.tail = &q.stub
	return q
}

// Push adds a processor to the queue. Lock-free, safe from any goroutine.
func (q *InjectQueue) Push(p *Processor) {
	node := injectNodePool.Get().(*injectNode)
	node.proc = p
	node.next.Store(nil)

	prev := q.head.Swap(node)
	prev.next.Store(node) // linearization point
}

// Pop removes and returns the oldest processor, or nil if empty.
// Only safe to call from the single consumer (the owning worker).
func (q *InjectQueue) Pop() *Processor {
	tail := q.tail
	next := tail.next.Load()
	if next == nil {
		return nil
	}

	q.tail = next
	proc := next.proc
	next.proc = nil

	// Recycle old tail node (never recycle stub)
	if tail != &q.stub {
		tail.next.Store(nil)
		injectNodePool.Put(tail)
	}

	return proc
}

// Drain pops all available processors into dst, returns count.
// Useful for batch processing.
func (q *InjectQueue) Drain(dst []*Processor) int {
	n := 0
	for n < len(dst) {
		p := q.Pop()
		if p == nil {
			break
		}
		dst[n] = p
		n++
	}
	return n
}
