package channel

import (
	"container/list"
	"errors"
	lua "github.com/yuin/gopher-lua"
)

// note, channel operations are not task safe but deterministic
type opKind int

const (
	sendOp opKind = iota
	receiveOp
)

type op struct {
	kind     opKind
	ch       *Channel
	value    lua.LValue
	chValue  lua.LValue  // Lua level value of channel, for select result
	task     *lua.LState // nil for buffered values
	selectOp *selectOp   // associated select op, nil for direct ops and buffered values
}

type selectOp struct {
	cases      []*op       // Cases to select from
	hasDefault bool        // If there is a default case
	task       *lua.LState // Task to wake
}

type onNext struct {
	yields  bool        // If current task should yield
	results []*opResult // Operations to wake
}

type opResult struct {
	err    error
	task   *lua.LState
	values []lua.LValue
}

// Channel represents a buffered or unbuffered channel, not-task safe, external synchronization is required.
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

func (c *Channel) send(senderTask *lua.LState, value lua.LValue, selectOp *selectOp) onNext {
	if c.closed {
		return onNext{
			results: []*opResult{
				{task: senderTask, err: errors.New("send on closed channel")},
			},
		}
	}

	// Try to wake receiver first
	if e := c.receivers.Front(); e != nil {
		recvOp := c.receivers.Remove(e).(*op)
		if recvOp.selectOp != nil {
			c.flushSelects(recvOp.selectOp) // clean all other channels involved in the select
			return onNext{
				yields: true,
				results: []*opResult{
					{task: recvOp.task, values: selectResult(recvOp.task, recvOp.chValue, recvOp.value, true)},
					{task: senderTask, values: callerResult(senderTask, selectOp, value, nil, true)},
				},
			}
		}

		if recvOp.task != nil {
			return onNext{
				yields: true,
				results: []*opResult{
					{task: recvOp.task, values: []lua.LValue{value, lua.LBool(true)}},
					{task: senderTask, values: callerResult(senderTask, selectOp, value, nil, true)}, // send ok
				},
			}
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
		return onNext{
			results: []*opResult{
				{task: senderTask, values: callerResult(senderTask, selectOp, value, nil, true)}, // send successful
			},
		}
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

	return onNext{yields: true} // Yield, nothing to do
}

func (c *Channel) receive(receiverTask *lua.LState, selectOp *selectOp) onNext {
	// Try get from senders first
	if e := c.senders.Front(); e != nil {
		sendOp := c.senders.Remove(e).(*op)
		c.size--

		if sendOp.selectOp != nil {
			c.flushSelects(sendOp.selectOp)

			return onNext{
				yields: true,
				results: []*opResult{
					{task: sendOp.task, values: nil}, // wake sender
					{task: receiverTask, values: callerResult(receiverTask, selectOp, sendOp.value, sendOp.value, true)},
				},
			}
		}

		if sendOp.task != nil {
			return onNext{
				yields: true,
				results: []*opResult{
					{task: sendOp.task, values: nil}, // wake sender
					{task: receiverTask, values: callerResult(receiverTask, selectOp, sendOp.value, sendOp.value, true)},
				},
			}
		}

		// buffered
		return onNext{
			results: []*opResult{
				{task: receiverTask, values: callerResult(receiverTask, selectOp, sendOp.value, sendOp.value, true)},
			},
		}
	}

	if c.closed {
		return onNext{
			results: []*opResult{
				{task: receiverTask, values: callerResult(receiverTask, selectOp, lua.LNil, lua.LNil, false)},
			},
		}
	}
	// Have to block
	c.receivers.PushBack(&op{
		kind:     receiveOp,
		ch:       c,
		task:     receiverTask,
		selectOp: selectOp,
	})
	return onNext{yields: true}
}

func (c *Channel) close() onNext {
	if c.closed {
		return onNext{
			results: []*opResult{
				{err: errors.New("close of closed channel")},
			},
		}
	}

	c.closed = true
	var results []*opResult

	// Handle and remove ALL senders - they all fail with error
	for e := c.senders.Front(); e != nil; {
		nextE := e.Next() // Save next before removing current
		op := e.Value.(*op)
		if op.selectOp != nil {
			c.flushSelects(op.selectOp) // deletes any other pending selects on other channels
		}
		if op.task != nil {
			results = append(results, &opResult{
				task: op.task,
				err:  errors.New("send on closed channel"),
			})
		}
		c.senders.Remove(e)
		c.size--
		e = nextE
	}

	// Handle non-buffered receivers only
	for e := c.receivers.Front(); e != nil; {
		nextE := e.Next() // Save next before potential removal
		op := e.Value.(*op)
		if op.task != nil {
			if op.selectOp != nil {
				c.flushSelects(op.selectOp)
			}
			results = append(results, &opResult{
				task:   op.task,
				values: []lua.LValue{lua.LNil, lua.LBool(false)},
			})
			c.receivers.Remove(e) // remove task receiver, buffered values are not removed
		}
		e = nextE
	}

	return onNext{
		yields:  len(results) > 0,
		results: results,
	}
}

func (c *Channel) flushSelects(s *selectOp) {
	for _, caseOp := range s.cases {
		if caseOp.ch != c {
			caseOp.ch.discardSelect(s)
		}
	}
}

func (c *Channel) discardSelect(selectOp *selectOp) {
	/* Good candidate for optimization. */
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

func (c *Channel) reset() {
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

func selectResult(L *lua.LState, chValue, value lua.LValue, ok bool) []lua.LValue {
	result := L.NewTable()
	result.RawSetString("channel", chValue)
	result.RawSetString("value", value)
	result.RawSetString("ok", lua.LBool(ok))

	return []lua.LValue{result}
}

func callerResult(task *lua.LState, selectOp *selectOp, chValue, value lua.LValue, ok bool) []lua.LValue {
	if selectOp != nil {
		return selectResult(task, chValue, value, ok)
	}

	if value == nil {
		return nil // For send operations that succeed
	}

	return []lua.LValue{value, lua.LBool(ok)} // For receive operations
}
