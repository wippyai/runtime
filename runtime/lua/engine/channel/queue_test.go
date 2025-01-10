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
		q := newPendingQueue()
		assert.Equal(t, 0, q.size())
		assert.Nil(t, q.dequeue())
	})

	t.Run("single element operations", func(t *testing.T) {
		q := newPendingQueue()
		op := &pendingOp{}

		q.enqueue(op)
		assert.Equal(t, 1, q.size())
		assert.NotNil(t, q.ops.Front())
		assert.Equal(t, op, q.ops.Front().Value)

		dequeued := q.dequeue()
		assert.Equal(t, op, dequeued)
		assert.Equal(t, 0, q.size())
		assert.Nil(t, q.ops.Front())
	})

	t.Run("multiple elements operations", func(t *testing.T) {
		q := newPendingQueue()
		op1 := &pendingOp{}
		op2 := &pendingOp{}
		op3 := &pendingOp{}

		q.enqueue(op1)
		q.enqueue(op2)
		q.enqueue(op3)

		assert.Equal(t, 3, q.size())

		dequeued1 := q.dequeue()
		assert.Equal(t, op1, dequeued1)
		assert.Equal(t, 2, q.size())

		dequeued2 := q.dequeue()
		assert.Equal(t, op2, dequeued2)
		assert.Equal(t, 1, q.size())

		dequeued3 := q.dequeue()
		assert.Equal(t, op3, dequeued3)
		assert.Equal(t, 0, q.size())
	})

	t.Run("select operation tracking", func(t *testing.T) {
		q := newPendingQueue()
		selectOp := &selectOperation{}
		op := &pendingOp{selectOp: selectOp}

		q.enqueue(op)
		assert.Len(t, q.selectOps[selectOp], 1)

		q.dequeue()
		assert.Empty(t, q.selectOps)
	})

	t.Run("remove operation", func(t *testing.T) {
		q := newPendingQueue()
		op := &pendingOp{}

		q.enqueue(op)
		success := q.remove(op)
		assert.True(t, success)
		assert.Equal(t, 0, q.size())

		success = q.remove(op)
		assert.False(t, success)
	})

	t.Run("clear queue", func(t *testing.T) {
		q := newPendingQueue()
		op1 := &pendingOp{}
		op2 := &pendingOp{selectOp: &selectOperation{}}

		q.enqueue(op1)
		q.enqueue(op2)
		q.clear()

		assert.Equal(t, 0, q.size())
		assert.Empty(t, q.selectOps)
	})
}

func TestQueueMapper(t *testing.T) {
	t.Run("allocate new queue", func(t *testing.T) {
		mapper := newQueueMapper()
		ch := &Channel{}

		queue := mapper.allocateQueue(ch)
		assert.NotNil(t, queue)
		assert.Equal(t, 0, queue.size())

		// Test reuse of same channel
		queue2 := mapper.allocateQueue(ch)
		assert.Equal(t, queue, queue2)
	})

	t.Run("named channel operations", func(t *testing.T) {
		mapper := newQueueMapper()
		ch := &Channel{name: "test"}
		op := &pendingOp{
			op: &chanOperation{ch: ch},
		}

		// Test enqueue
		mapper.enqueue(ch, op)
		assert.Equal(t, 1, mapper.getQueueSize(ch))
		assert.Equal(t, 1, mapper.getNamedQueueSize("test"))

		// Test dequeue by name
		dequeued := mapper.dequeueNamed("test")
		assert.Equal(t, op, dequeued)
		assert.Equal(t, 0, mapper.getQueueSize(ch))
		assert.Equal(t, 0, mapper.getNamedQueueSize("test"))
	})

	t.Run("regular channel operations", func(t *testing.T) {
		mapper := newQueueMapper()
		ch := &Channel{}
		op := &pendingOp{
			op: &chanOperation{ch: ch},
		}

		mapper.enqueue(ch, op)
		assert.Equal(t, 1, mapper.getQueueSize(ch))

		dequeued := mapper.dequeue(ch)
		assert.Equal(t, op, dequeued)
		assert.Equal(t, 0, mapper.getQueueSize(ch))
	})

	t.Run("select operation removal", func(t *testing.T) {
		mapper := newQueueMapper()
		ch := &Channel{}
		selectOp := &selectOperation{}
		op := &pendingOp{
			op:       &chanOperation{ch: ch},
			selectOp: selectOp,
		}

		mapper.enqueue(ch, op)
		mapper.removeSelect(selectOp)
		assert.Equal(t, 0, mapper.getQueueSize(ch))
	})

	t.Run("queue cleanup", func(t *testing.T) {
		mapper := newQueueMapper()
		ch1 := &Channel{name: "test1"}
		ch2 := &Channel{name: "test2"}

		mapper.enqueue(ch1, &pendingOp{op: &chanOperation{ch: ch1}})
		mapper.enqueue(ch2, &pendingOp{op: &chanOperation{ch: ch2}})

		mapper.clear()
		assert.Equal(t, 0, mapper.getQueueSize(ch1))
		assert.Equal(t, 0, mapper.getQueueSize(ch2))
		assert.Equal(t, 0, mapper.getNamedQueueSize("test1"))
		assert.Equal(t, 0, mapper.getNamedQueueSize("test2"))
	})
}

func TestEdgeCases(t *testing.T) {
	t.Run("remove nil op", func(t *testing.T) {
		q := newPendingQueue()
		success := q.remove(nil)
		assert.False(t, success)
	})

	t.Run("removeSelect with nil selectOp", func(t *testing.T) {
		q := newPendingQueue()
		q.removeSelect(nil) // Should not panic
	})

	t.Run("reset with nil ops list", func(t *testing.T) {
		q := &pendingQueue{} // manually create without initialization
		q.reset()            // should handle nil ops
		assert.NotNil(t, q.ops)
	})
}

func TestQueueMapperExtended(t *testing.T) {
	t.Run("dequeue from non-existent channel", func(t *testing.T) {
		mapper := newQueueMapper()
		op := mapper.dequeue(&Channel{})
		assert.Nil(t, op)
	})

	t.Run("dequeueNamed from non-existent name", func(t *testing.T) {
		mapper := newQueueMapper()
		op := mapper.dequeueNamed("nonexistent")
		assert.Nil(t, op)
	})

	t.Run("cleanup on empty dequeue", func(t *testing.T) {
		mapper := newQueueMapper()
		ch := &Channel{name: "test"}
		op := &pendingOp{op: &chanOperation{ch: ch}}

		// Add and then remove via mapper methods
		mapper.enqueue(ch, op)
		_ = mapper.dequeue(ch) // This should trigger cleanup since queue becomes empty

		// Verify cleanup
		_, exists := mapper.queues[ch]
		assert.False(t, exists, "channel queue should be cleaned up")
		_, existsNamed := mapper.named["test"]
		assert.False(t, existsNamed, "named queue should be cleaned up")
	})
}

func TestPoolReuse(t *testing.T) {
	t.Run("pool reuse safety", func(t *testing.T) {
		op := pendingPool.Get().(*pendingOp)
		op.task = &engine.Task{}

		op.reset()
		pendingPool.Put(op)

		newOp := pendingPool.Get().(*pendingOp)
		assert.Nil(t, newOp.task, "pool should return clean objects")
	})
}

func TestQueueState(t *testing.T) {
	t.Run("queue state after operations", func(t *testing.T) {
		mapper := newQueueMapper()
		ch := &Channel{name: "test"}
		op := &pendingOp{op: &chanOperation{ch: ch}}

		mapper.enqueue(ch, op)
		_ = mapper.dequeue(ch)

		// Try to reuse the same channel
		mapper.enqueue(ch, op)
		assert.Equal(t, 1, mapper.getQueueSize(ch))
		assert.Equal(t, 1, mapper.getNamedQueueSize("test"))
	})
}

func TestSelectOperations(t *testing.T) {
	t.Run("multiple select ops tracking", func(t *testing.T) {
		q := newPendingQueue()
		select1 := &selectOperation{}
		select2 := &selectOperation{}

		op1 := &pendingOp{selectOp: select1}
		op2 := &pendingOp{selectOp: select2}

		q.enqueue(op1)
		q.enqueue(op2)

		assert.Len(t, q.selectOps[select1], 1)
		assert.Len(t, q.selectOps[select2], 1)

		q.removeSelect(select1)
		assert.Len(t, q.selectOps[select2], 1)
	})
}

func TestNamedChannelAliasing(t *testing.T) {
	t.Run("named channel aliasing", func(t *testing.T) {
		mapper := newQueueMapper()
		ch := &Channel{name: "test"}

		queue1 := mapper.allocateQueue(ch)
		namedQueue := mapper.named["test"]

		assert.Equal(t, queue1, namedQueue, "named queue should be aliased to channel queue")
	})
}

func BenchmarkQueueOperations(b *testing.B) {
	b.Run("enqueue/dequeue cycle", func(b *testing.B) {
		mapper := newQueueMapper()
		ch := &Channel{}
		op := &pendingOp{
			op: &chanOperation{ch: ch},
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			mapper.enqueue(ch, op)
			mapper.dequeue(ch)
		}
	})

	b.Run("named channel operations", func(b *testing.B) {
		mapper := newQueueMapper()
		ch := &Channel{name: "test"}
		op := &pendingOp{
			op: &chanOperation{ch: ch},
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			mapper.enqueue(ch, op)
			mapper.dequeueNamed("test")
		}
	})

	b.Run("select operation", func(b *testing.B) {
		mapper := newQueueMapper()
		ch := &Channel{}
		selectOp := &selectOperation{}
		op := &pendingOp{
			op:       &chanOperation{ch: ch},
			selectOp: selectOp,
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			mapper.enqueue(ch, op)
			mapper.removeSelect(selectOp)
		}
	})
}
