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
	task     *lua.LState // nil for buffered values
	selectOp *selectOp   // associated select op, nil for direct ops and buffered values
}

type selectOp struct {
	cases      []*op       // Cases to select from
	hasDefault bool        // If there is a default case
	task       *lua.LState // Task to wake
}

type onNext struct {
	yields  bool       // If current task should yield
	next    []*opStep  // Operations to wake
	block   []*Channel // Channels this task is waiting on
	release []*Channel // Channels this task is releasing
}

type opStep struct {
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

	value     lua.LValue // lua value associated with the channel
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

func (c *Channel) send(senderTask *lua.LState, value lua.LValue, selectOp *selectOp) *onNext {
	if c.closed {
		return &onNext{
			next: []*opStep{
				{task: senderTask, err: errors.New("send on closed channel")},
			},
		}
	}

	// Try to wake receiver first
	if e := c.receivers.Front(); e != nil {
		recvOp := c.receivers.Remove(e).(*op)

		if recvOp.task != nil {
			return &onNext{
				yields: true,
				next: []*opStep{
					{
						task:   recvOp.task,
						values: makeResult(recvOp.task, recvOp.selectOp, recvOp.ch.value, value, true),
					},
					{
						task:   senderTask,
						values: makeResult(senderTask, selectOp, c.value, nil, true),
					},
				},
				release: release(recvOp),
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
		return &onNext{
			next: []*opStep{
				{
					task:   senderTask,
					values: makeResult(senderTask, selectOp, c.value, value, true),
				},
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

	return &onNext{yields: true, block: []*Channel{c}} // Yield, nothing to do
}

func (c *Channel) receive(receiverTask *lua.LState, selectOp *selectOp) *onNext {
	// Try get from senders first
	if e := c.senders.Front(); e != nil {
		sendOp := c.senders.Remove(e).(*op)
		c.size--

		if sendOp.task != nil {
			return &onNext{
				yields: true,
				next: []*opStep{
					{
						task:   sendOp.task,
						values: makeResult(sendOp.task, sendOp.selectOp, sendOp.ch.value, sendOp.value, true),
					},
					{
						task:   receiverTask,
						values: makeResult(receiverTask, selectOp, c.value, sendOp.value, true),
					},
				},
				release: release(sendOp),
			}
		}

		// buffered
		return &onNext{
			next: []*opStep{
				{
					task:   receiverTask,
					values: makeResult(receiverTask, selectOp, c.value, sendOp.value, true),
				},
			},
		}
	}

	if c.closed {
		return &onNext{
			next: []*opStep{
				{
					task:   receiverTask,
					values: makeResult(receiverTask, selectOp, c.value, lua.LNil, false),
				},
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
	return &onNext{yields: true, block: []*Channel{c}}
}

func (c *Channel) close(closerTask *lua.LState) *onNext {
	if c.closed {
		return &onNext{
			next: []*opStep{
				{err: errors.New("close of closed channel")},
			},
		}
	}

	c.closed = true
	var results []*opStep
	releases := make([]*Channel, 0)

	// Handle and remove ALL senders - they all fail with error
	for e := c.senders.Front(); e != nil; {
		nextE := e.Next() // Save next before removing current
		op := e.Value.(*op)
		if op.task != nil {
			results = append(results, &opStep{
				task: op.task,
				err:  errors.New("send on closed channel"),
			})
			releases = append(releases, release(op)...)
			c.senders.Remove(e)
			c.size--
		}
		e = nextE
	}

	// Handle non-buffered receivers only
	for e := c.receivers.Front(); e != nil; {
		nextE := e.Next()
		op := e.Value.(*op)
		if op.task != nil {
			results = append(results, &opStep{
				task:   op.task,
				values: makeResult(op.task, op.selectOp, c.value, lua.LNil, false),
			})
			releases = append(releases, release(op)...)
			c.receivers.Remove(e)
		}
		e = nextE
	}

	if len(results) > 0 {
		// wake up closer after all senders and non-buffered receivers are handled
		results = append(results, &opStep{task: closerTask, values: nil})
	}

	return &onNext{
		yields:  len(results) > 0,
		next:    results,
		release: releases,
	}
}

func release(op *op) []*Channel {
	if op.ch == nil {
		return nil
	}

	var releases []*Channel
	if op.selectOp != nil {
		releases = flushSelects(op.selectOp)
	} else {
		releases = []*Channel{op.ch}
	}

	return releases
}

func flushSelects(s *selectOp) []*Channel {
	var releases []*Channel
	for _, caseOp := range s.cases {
		releases = append(releases, caseOp.ch)
		caseOp.ch.discardSelect(s)
	}

	return releases
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

func makeResult(task *lua.LState, selectOp *selectOp, chValue, value lua.LValue, ok bool) []lua.LValue {
	if selectOp != nil {
		return selectResult(task, chValue, value, ok)
	}

	if value == nil {
		return nil // For send operations that succeed
	}

	return []lua.LValue{value, lua.LBool(ok)} // For receive operations
}

func selectResult(L *lua.LState, chValue, value lua.LValue, ok bool) []lua.LValue {
	result := L.NewTable()
	result.RawSetString("channel", chValue)
	result.RawSetString("value", value)
	result.RawSetString("ok", lua.LBool(ok))

	return []lua.LValue{result}
}
