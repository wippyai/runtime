package channel

import (
	"container/list"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
)

type opKind int

const (
	sendOp opKind = iota
	receiveOp
)

type op struct {
	kind     opKind
	ch       *Channel
	value    lua.LValue
	task     *engine.Task // nil for buffered values
	selectOp *selectOp    // nil for direct ops and buffered values
}

type selectOp struct {
	cases []*op
	task  *engine.Task
}

type opMatch struct {
	value     lua.LValue     // The value being transferred
	selectOp  *selectOp      // Select operation to complete
	wakeTasks []*engine.Task // Tasks to wake (if any)
	yields    bool           // If current task should yield
	closed    bool           // If operation was on closed channel
}

type Channel struct {
	name     string
	capacity int
	closed   bool
	size     int

	senders   *list.List
	receivers *list.List
}

func Named(name string, capacity int) *Channel {
	ch := newChannel(capacity)
	ch.name = name
	return ch
}

func newChannel(capacity int) *Channel {
	return &Channel{
		capacity:  capacity,
		senders:   list.New(),
		receivers: list.New(),
	}
}

func (c *Channel) trySend(senderTask *engine.Task, value lua.LValue, selectOp *selectOp) opMatch {
	if c.closed {
		return opMatch{closed: true}
	}

	// Try to wake receiver first
	if e := c.receivers.Front(); e != nil {
		recvOp := c.receivers.Remove(e).(*op)
		if recvOp.selectOp != nil {
			return opMatch{
				wakeTasks: []*engine.Task{recvOp.task, senderTask},
				selectOp:  recvOp.selectOp,
				value:     value,
				yields:    true,
			}
		}

		return opMatch{
			wakeTasks: []*engine.Task{recvOp.task, senderTask},
			value:     value,
			yields:    true,
		}
	}

	if c.size < c.capacity {
		c.senders.PushBack(&op{
			task:     nil,
			kind:     sendOp,
			ch:       c,
			value:    value,
			selectOp: selectOp,
		})
		c.size++
		return opMatch{} // No yield needed
	}

	// Have to block
	c.senders.PushBack(&op{
		task:     senderTask,
		kind:     sendOp,
		ch:       c,
		value:    value,
		selectOp: selectOp,
	})
	c.size++

	return opMatch{yields: true}
}

func (c *Channel) tryReceive(receiverTask *engine.Task, selectOp *selectOp) opMatch {
	// Try get from senders first
	if e := c.senders.Front(); e != nil {
		sendOp := c.senders.Remove(e).(*op)
		c.size--

		if sendOp.selectOp != nil {
			return opMatch{
				wakeTasks: []*engine.Task{sendOp.task, receiverTask},
				selectOp:  sendOp.selectOp,
				value:     sendOp.value,
				yields:    true,
			}
		}

		if sendOp.task != nil {
			return opMatch{
				wakeTasks: []*engine.Task{sendOp.task, receiverTask},
				value:     sendOp.value,
				yields:    true,
			}
		}

		// buffered
		return opMatch{value: sendOp.value}
	}

	if c.closed {
		return opMatch{closed: true}
	}

	// Have to block
	c.receivers.PushBack(&op{
		kind:     receiveOp,
		ch:       c,
		task:     receiverTask,
		selectOp: selectOp,
	})
	return opMatch{yields: true}
}

func (c *Channel) close() opMatch {
	if c.closed {
		return opMatch{closed: true}
	}
	c.closed = true

	senderIter := c.senders.Front()
	receiverIter := c.receivers.Front()

	var tasksToWake []*engine.Task

	// Process both lists until we exhaust one or both
	for senderIter != nil || receiverIter != nil {
		if senderIter != nil {
			op := senderIter.Value.(*op)
			if op.task != nil { // Only wake if it's not a buffered value
				tasksToWake = append(tasksToWake, op.task)
			}
			senderIter = senderIter.Next()
		}

		if receiverIter != nil {
			tasksToWake = append(tasksToWake, receiverIter.Value.(*op).task)
			receiverIter = receiverIter.Next()
		}
	}

	// Cleanup channel state
	c.cleanup()

	return opMatch{
		wakeTasks: tasksToWake,
		closed:    true,
		yields:    true, // to wake tasks
	}
}

func (c *Channel) discardSelect(selectOp *selectOp) {
	for e := c.senders.Front(); e != nil; {
		next := e.Next()
		if op := e.Value.(*op); op.selectOp == selectOp {
			c.senders.Remove(e)
			c.size--
		}
		e = next
	}

	for e := c.receivers.Front(); e != nil; {
		next := e.Next()
		if op := e.Value.(*op); op.selectOp == selectOp {
			c.receivers.Remove(e)
		}
		e = next
	}
}

func (c *Channel) cleanup() {
	c.size = 0
	c.senders.Init()
	c.receivers.Init()
}

func (c *Channel) isFull() bool {
	return c.size >= c.capacity
}

func (c *Channel) isEmpty() bool {
	return c.size == 0
}

func (c *Channel) isNamed() bool {
	return c.name != ""
}

func (c *Channel) canSend() bool {
	return c.receivers.Front() != nil || (!c.closed && c.size < c.capacity)
}

func (c *Channel) canReceive() bool {
	return c.senders.Front() != nil
}
