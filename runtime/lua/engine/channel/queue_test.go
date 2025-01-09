package channel

import (
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestPendingOp(t *testing.T) {
	t.Run("reset clears all fields", func(t *testing.T) {
		op := &pendingOp{
			task:     &engine.Task{},
			op:       &chanOperation{},
			next:     &pendingOp{},
			selectOp: &selectOperation{},
		}

		op.reset()

		assert.Nil(t, op.task)
		assert.Nil(t, op.op)
		assert.Nil(t, op.next)
		assert.Nil(t, op.selectOp)
	})
}

func TestPendingQueue(t *testing.T) {
	t.Run("empty queue operations", func(t *testing.T) {
		q := &pendingQueue{}

		assert.Equal(t, 0, q.size)
		assert.Nil(t, q.dequeue())
		assert.Nil(t, q.head)
		assert.Nil(t, q.tail)
	})

	t.Run("single element operations", func(t *testing.T) {
		q := &pendingQueue{}
		op := &pendingOp{}

		q.enqueue(op)
		assert.Equal(t, 1, q.size)
		assert.Equal(t, op, q.head)
		assert.Equal(t, op, q.tail)

		dequeued := q.dequeue()
		assert.Equal(t, op, dequeued)
		assert.Equal(t, 0, q.size)
		assert.Nil(t, q.head)
		assert.Nil(t, q.tail)
	})

	t.Run("multiple elements operations", func(t *testing.T) {
		q := &pendingQueue{}
		op1 := &pendingOp{}
		op2 := &pendingOp{}
		op3 := &pendingOp{}

		q.enqueue(op1)
		q.enqueue(op2)
		q.enqueue(op3)

		assert.Equal(t, 3, q.size)
		assert.Equal(t, op1, q.head)
		assert.Equal(t, op3, q.tail)

		dequeued1 := q.dequeue()
		assert.Equal(t, op1, dequeued1)
		assert.Equal(t, 2, q.size)
		assert.Equal(t, op2, q.head)

		dequeued2 := q.dequeue()
		assert.Equal(t, op2, dequeued2)
		assert.Equal(t, 1, q.size)
		assert.Equal(t, op3, q.head)
		assert.Equal(t, op3, q.tail)

		dequeued3 := q.dequeue()
		assert.Equal(t, op3, dequeued3)
		assert.Equal(t, 0, q.size)
		assert.Nil(t, q.head)
		assert.Nil(t, q.tail)
	})

	t.Run("clear queue", func(t *testing.T) {
		q := &pendingQueue{}
		op1 := &pendingOp{}
		op2 := &pendingOp{}

		q.enqueue(op1)
		q.enqueue(op2)
		q.clear()

		assert.Equal(t, 0, q.size)
		assert.Nil(t, q.head)
		assert.Nil(t, q.tail)
	})

	t.Run("reset queue", func(t *testing.T) {
		q := &pendingQueue{}
		op := &pendingOp{}

		q.enqueue(op)
		q.reset()

		assert.Equal(t, 0, q.size)
		assert.Nil(t, q.head)
		assert.Nil(t, q.tail)
	})
}

func TestQueueMapper(t *testing.T) {
	t.Run("allocate new queue", func(t *testing.T) {
		mapper := newQueueMapper()
		ch := &Channel{}

		queue := mapper.allocateQueue(ch)
		assert.NotNil(t, queue)
		assert.Equal(t, 0, queue.size)

		// Test reuse of same channel
		queue2 := mapper.allocateQueue(ch)
		assert.Equal(t, queue, queue2)
	})

	t.Run("enqueue and dequeue operations", func(t *testing.T) {
		mapper := newQueueMapper()
		ch := &Channel{}
		op := &pendingOp{}

		mapper.enqueue(ch, op)
		assert.Equal(t, 1, mapper.getQueueSize(ch))

		dequeued := mapper.dequeue(ch)
		assert.Equal(t, op, dequeued)
		assert.Equal(t, 0, mapper.getQueueSize(ch))
	})

	t.Run("queue cleanup on empty", func(t *testing.T) {
		mapper := newQueueMapper()
		ch := &Channel{}
		op := &pendingOp{}

		mapper.enqueue(ch, op)
		mapper.dequeue(ch)

		// Queue should be removed from mapper and returned to pool
		_, exists := mapper.queues[ch]
		assert.False(t, exists)
		assert.Equal(t, 0, mapper.getQueueSize(ch))
	})

	t.Run("clear all queues", func(t *testing.T) {
		mapper := newQueueMapper()
		ch1 := &Channel{}
		ch2 := &Channel{}

		mapper.enqueue(ch1, &pendingOp{})
		mapper.enqueue(ch2, &pendingOp{})
		assert.Equal(t, 2, len(mapper.queues))

		mapper.clear()
		assert.Equal(t, 0, len(mapper.queues))
		assert.Equal(t, 0, mapper.getQueueSize(ch1))
		assert.Equal(t, 0, mapper.getQueueSize(ch2))
	})

	t.Run("dequeue from non-existent queue", func(t *testing.T) {
		mapper := newQueueMapper()
		ch := &Channel{}

		op := mapper.dequeue(ch)
		assert.Nil(t, op)
	})
}

// Benchmarks
func BenchmarkPendingQueue(b *testing.B) {
	b.Run("enqueue", func(b *testing.B) {
		q := &pendingQueue{}
		op := &pendingOp{}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			q.enqueue(op)
		}
	})

	b.Run("dequeue", func(b *testing.B) {
		q := &pendingQueue{}
		op := &pendingOp{}

		// Pre-fill queue
		for i := 0; i < b.N; i++ {
			q.enqueue(op)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			q.dequeue()
		}
	})

	b.Run("enqueue_dequeue_cycle", func(b *testing.B) {
		q := &pendingQueue{}
		op := &pendingOp{}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			q.enqueue(op)
			q.dequeue()
		}
	})
}

func BenchmarkQueueMapper(b *testing.B) {
	b.Run("allocateQueue", func(b *testing.B) {
		mapper := newQueueMapper()
		channels := make([]*Channel, b.N)
		for i := 0; i < b.N; i++ {
			channels[i] = &Channel{}
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			mapper.allocateQueue(channels[i])
		}
	})

	b.Run("enqueue_dequeue", func(b *testing.B) {
		mapper := newQueueMapper()
		ch := &Channel{}
		op := &pendingOp{}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			mapper.enqueue(ch, op)
			mapper.dequeue(ch)
		}
	})
}

// Test object pool behavior
func TestObjectPool(t *testing.T) {
	t.Run("pendingPool reuse", func(t *testing.T) {
		// Get an object from the pool
		op1 := pendingPool.Get().(*pendingOp)
		op1.task = &engine.Task{}

		// Reset and return it to the pool
		op1.reset()
		pendingPool.Put(op1)

		// Get another object
		op2 := pendingPool.Get().(*pendingOp)

		// Should be reset
		assert.Nil(t, op2.task)
	})

	t.Run("queuePool reuse", func(t *testing.T) {
		// Get a queue from the pool
		q1 := queuePool.Get().(*pendingQueue)
		q1.enqueue(&pendingOp{})

		// Return it to the pool
		q1.clear()
		queuePool.Put(q1)

		// Get another queue
		q2 := queuePool.Get().(*pendingQueue)

		// Should be reset
		assert.Equal(t, 0, q2.size)
		assert.Nil(t, q2.head)
		assert.Nil(t, q2.tail)
	})
}
