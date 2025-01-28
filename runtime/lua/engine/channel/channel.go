package channel

import (
	"container/list"
	"errors"

	lua "github.com/yuin/gopher-lua"
)

// note, channel operations are not state safe but deterministic
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
	yields  bool       // If current state should yield
	next    []*opStep  // Operations to wake
	block   []*Channel // Channels this state is waiting on
	release []*Channel // Channels this state is releasing
}

type opStep struct {
	err    error
	state  *lua.LState
	values []lua.LValue
}

// Channel represents a buffered or unbuffered channel that requires external synchronization.
// It implements Lua channel semantics with support for buffering, select operations,
// and non-blocking sends/receives.
type Channel struct {
	name     string
	capacity int
	closed   bool
	size     int

	value     lua.LValue // lua value associated with the channel
	senders   *list.List
	receivers *list.List
}

// Named creates a new channel with the given name and buffer capacity.
// Named channels can be referenced across different Lua states.
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

// Name returns the name of the channel.
func (c *Channel) Name() string {
	return c.name
}

// Slots returns the number of available slots for sending.
// This includes both buffer capacity and any waiting receivers.
func (c *Channel) Slots() int {
	return (c.capacity - c.size) + c.receivers.Len()
}

// Value returns the Lua value associated with the channel.
func (c *Channel) Value() lua.LValue {
	return c.value
}

func (c *Channel) send(senderTask *lua.LState, value lua.LValue, selectOp *selectOp) *onNext {
	if c.closed {
		return &onNext{
			next: []*opStep{
				{state: senderTask, err: errors.New("send on closed channel")},
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
						state:  recvOp.task,
						values: makeResult(recvOp.task, recvOp.selectOp, recvOp.ch.value, value, true),
					},
					{
						state:  senderTask,
						values: makeResult(senderTask, selectOp, c.value, nil, true),
					},
				},
				release: append(release(recvOp), flushSelects(selectOp)...),
			}
		}
	}

	if c.size < c.capacity {
		c.senders.PushBack(&op{
			task:     nil,
			kind:     sendOp,
			ch:       c,
			value:    value,
			selectOp: nil,
		})
		c.size++

		return &onNext{
			next: []*opStep{
				{
					state:  senderTask,
					values: makeResult(senderTask, selectOp, c.value, value, true),
				},
			},
			release: flushSelects(selectOp),
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

	return &onNext{yields: true, block: []*Channel{c}}
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
						state:  sendOp.task,
						values: makeResult(sendOp.task, sendOp.selectOp, sendOp.ch.value, sendOp.value, true),
					},
					{
						state:  receiverTask,
						values: makeResult(receiverTask, selectOp, c.value, sendOp.value, true),
					},
				},
				release: append(release(sendOp), flushSelects(selectOp)...),
			}
		}

		// buffered
		return &onNext{
			next: []*opStep{
				{
					state:  receiverTask,
					values: makeResult(receiverTask, selectOp, c.value, sendOp.value, true),
				},
			},
		}
	}

	if c.closed {
		return &onNext{
			next: []*opStep{
				{
					state:  receiverTask,
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
		nextE := e.Next()
		op := e.Value.(*op)
		if op.task != nil {
			results = append(results, &opStep{
				state: op.task,
				err:   errors.New("send on closed channel"),
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
				state:  op.task,
				values: makeResult(op.task, op.selectOp, c.value, lua.LNil, false),
			})
			releases = append(releases, release(op)...)
			c.receivers.Remove(e)
		}
		e = nextE
	}

	if len(results) > 0 {
		results = append(results, &opStep{state: closerTask, values: nil})
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

func (c *Channel) discardSelect(selectOp *selectOp) {
	// First, check senders
	for e := c.senders.Front(); e != nil; {
		next := e.Next()
		if op := e.Value.(*op); op.selectOp == selectOp {
			c.senders.Remove(e)
			c.size--
			break
		}
		e = next
	}

	for e := c.receivers.Front(); e != nil; {
		next := e.Next()
		op := e.Value.(*op)

		if op.selectOp == selectOp {
			c.receivers.Remove(e)
			break
		}
		e = next
	}
}

func flushSelects(s *selectOp) []*Channel {
	if s == nil {
		return nil
	}

	releases := make([]*Channel, 0, len(s.cases))
	for _, caseOp := range s.cases {
		if caseOp.ch == nil {
			continue
		}

		releases = append(releases, caseOp.ch)
		caseOp.ch.discardSelect(s)
	}

	return releases
}

// todo: unused
func (c *Channel) reset() { //nolint:unused
	c.size = 0
	c.senders.Init()
	c.receivers.Init()
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
		return nil
	}

	return []lua.LValue{value, lua.LBool(ok)}
}

func selectResult(l *lua.LState, chValue, value lua.LValue, ok bool) []lua.LValue {
	result := l.NewTable()
	result.RawSetString("channel", chValue)
	result.RawSetString("value", value)
	result.RawSetString("ok", lua.LBool(ok))

	return []lua.LValue{result}
}
