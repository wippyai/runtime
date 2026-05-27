// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"container/list"
	"errors"
	"sync"

	lua "github.com/wippyai/go-lua"
)

// TaskUpdate represents a task state change from channel operation
type TaskUpdate struct {
	State     *lua.LState
	Error     error
	resultBuf [3]lua.LValue
	resultLen int
}

func (u *TaskUpdate) GetResult() []lua.LValue {
	return u.resultBuf[:u.resultLen]
}

func (u *TaskUpdate) setResult1(v lua.LValue) {
	u.resultBuf[0] = v
	u.resultLen = 1
}

func (u *TaskUpdate) setResult2(v1, v2 lua.LValue) {
	u.resultBuf[0] = v1
	u.resultBuf[1] = v2
	u.resultLen = 2
}

func (u *TaskUpdate) setSelectResult(l *lua.LState, ch, value lua.LValue, ok bool) {
	result := l.CreateTable(0, 3)
	result.RawSetString("channel", ch)
	result.RawSetString("value", value)
	result.RawSetString("ok", lua.LBool(ok))
	u.resultBuf[0] = result
	u.resultLen = 1
}

func (u *TaskUpdate) reset() {
	u.State = nil
	u.Error = nil
	u.resultBuf[0] = nil
	u.resultBuf[1] = nil
	u.resultBuf[2] = nil
	u.resultLen = 0
}

// ChannelResult is the outcome of a channel operation
type ChannelResult struct {
	Updates []*TaskUpdate
	Block   []*Channel
	Release []*Channel
	Yields  bool
}

func (r *ChannelResult) String() string       { return "<channel_result>" }
func (r *ChannelResult) Type() lua.LValueType { return lua.LTUserData }

func (r *ChannelResult) GetUpdates() []*TaskUpdate {
	return r.Updates
}

func (r *ChannelResult) reset() {
	r.Yields = false
	r.Updates = r.Updates[:0]
	r.Block = r.Block[:0]
	r.Release = r.Release[:0]
}

// ChannelOpKind identifies send vs receive operations
type ChannelOpKind int

const (
	SendOp ChannelOpKind = iota
	ReceiveOp
)

// ChannelOp is a public channel operation for select cases
type ChannelOp struct {
	Value    lua.LValue
	Channel  *Channel
	Task     *lua.LState
	SelectOp *SelectOp
	Kind     ChannelOpKind
}

// SelectOp represents a select operation across multiple channels
type SelectOp struct {
	Task       *lua.LState
	Cases      []*ChannelOp
	HasDefault bool
}

// chanOp is an internal channel operation
type chanOp struct {
	value    lua.LValue
	channel  *Channel
	task     *lua.LState
	selectOp *SelectOp
	isSend   bool
}

// Channel represents a Go-like channel. Not thread-safe - caller owns synchronization.
type Channel struct {
	value    lua.LValue
	buffer   *list.List
	sendq    *list.List
	recvq    *list.List
	capacity int
	closed   bool
}

func NewChannel(capacity int) *Channel {
	if capacity < 0 {
		capacity = 0
	}
	return &Channel{
		capacity: capacity,
		buffer:   list.New(),
		sendq:    list.New(),
		recvq:    list.New(),
	}
}

func (c *Channel) Value() lua.LValue     { return c.value }
func (c *Channel) SetValue(v lua.LValue) { c.value = v }
func (c *Channel) IsClosed() bool        { return c.closed }
func (c *Channel) Size() int             { return c.buffer.Len() + c.sendq.Len() }

func (c *Channel) Slots() int {
	return (c.capacity - c.buffer.Len()) + c.recvq.Len()
}

func (c *Channel) CanSend() bool {
	return c.recvq.Len() > 0 || (!c.closed && c.buffer.Len() < c.capacity)
}

func (c *Channel) CanReceive() bool {
	return c.buffer.Len() > 0 || c.sendq.Len() > 0 || c.closed
}

func (c *Channel) Send(task *lua.LState, value lua.LValue, selectOp *SelectOp) *ChannelResult {
	if c.closed {
		return errorResult(task, errors.New("send on closed channel"))
	}

	sel := toInternalSelect(selectOp)

	// Case 1: receiver waiting - direct handoff
	if e := c.recvq.Front(); e != nil {
		op := c.recvq.Remove(e).(*chanOp)
		return c.handoff(task, value, sel, op)
	}

	// Case 2: buffer has space
	if c.buffer.Len() < c.capacity {
		c.buffer.PushBack(value)
		return c.senderDone(task, value, sel)
	}

	// Case 3: must block
	return c.blockSender(task, value, sel)
}

// TrySend attempts a nonblocking send. It hands off to a waiting receiver if
// one is queued, otherwise pushes to the buffer if there is room, otherwise
// reports sent=false WITHOUT pushing a phantom blocked-sender into sendq.
//
// Returns (ChannelResult, sent). The result may be nil when no task updates
// are needed (e.g., a successful buffered push or an overflowed send).
// Callers must release any non-nil result via ReleaseResult.
//
// Used by external producers (deliverMessage's subscription delivery path,
// the ephemeral channel router) that must never block on a Lua channel
// because there is no real producer task that could be woken.
func (c *Channel) TrySend(value lua.LValue) (*ChannelResult, bool) {
	if c.closed {
		return errorResult(nil, errors.New("send on closed channel")), false
	}

	// Case 1: receiver waiting - direct handoff. External producers do not
	// have a sender task to wake, so we craft a minimal result that wakes
	// the receiver and accounts for the channel's block/release bookkeeping
	// via flushSelect (matches handoff's Release output).
	if e := c.recvq.Front(); e != nil {
		op := c.recvq.Remove(e).(*chanOp)
		defer releaseChanOp(op)

		r := acquireResult()
		r.Yields = true
		uRecv := acquireTaskUpdate()
		uRecv.State = op.task
		if op.selectOp != nil {
			uRecv.setSelectResult(op.task, c.value, value, true)
		} else {
			uRecv.setResult2(value, lua.LTrue)
		}
		r.Updates = append(r.Updates, uRecv)
		r.Release = append(r.Release, c)
		r.Release = append(r.Release, c.flushSelect(op.selectOp)...)
		return r, true
	}

	// Case 2: buffer has space.
	if c.buffer.Len() < c.capacity {
		c.buffer.PushBack(value)
		return nil, true
	}

	// Case 3: full and no waiter. Drop on the producer side rather than
	// creating a fake blocked-sender entry.
	return nil, false
}

func (c *Channel) Receive(task *lua.LState, selectOp *SelectOp) *ChannelResult {
	sel := toInternalSelect(selectOp)

	// Case 1: buffer has value
	if c.buffer.Len() > 0 {
		value := c.buffer.Remove(c.buffer.Front()).(lua.LValue)

		// If blocked sender waiting, their value fills the freed slot
		if e := c.sendq.Front(); e != nil {
			op := c.sendq.Remove(e).(*chanOp)
			c.buffer.PushBack(op.value)
			return c.recvWithSenderWake(task, value, sel, op)
		}
		return c.receiverDone(task, value, sel)
	}

	// Case 2: blocked sender waiting - direct handoff
	if e := c.sendq.Front(); e != nil {
		op := c.sendq.Remove(e).(*chanOp)
		return c.recvHandoff(task, sel, op)
	}

	// Case 3: closed channel
	if c.closed {
		return c.receiverClosed(task, sel)
	}

	// Case 4: must block
	return c.blockReceiver(task, sel)
}

// Drain discards all buffered values from the channel without closing it.
// Returns the number of items discarded. Not thread-safe.
func (c *Channel) Drain() int {
	n := c.buffer.Len()
	c.buffer.Init()
	return n
}

func (c *Channel) Close(caller *lua.LState) *ChannelResult {
	if c.closed {
		return errorResult(caller, errors.New("close of closed channel"))
	}
	c.closed = true

	r := acquireResult()

	// Error all blocked senders
	for c.sendq.Len() > 0 {
		op := c.sendq.Remove(c.sendq.Front()).(*chanOp)
		u := acquireTaskUpdate()
		u.State = op.task
		u.Error = errors.New("send on closed channel")
		r.Updates = append(r.Updates, u)
		r.Release = append(r.Release, c)
		r.Release = append(r.Release, c.flushSelect(op.selectOp)...)
		releaseChanOp(op)
	}

	// Wake all blocked receivers with nil
	for c.recvq.Len() > 0 {
		op := c.recvq.Remove(c.recvq.Front()).(*chanOp)
		u := acquireTaskUpdate()
		u.State = op.task
		if op.selectOp != nil {
			u.setSelectResult(op.task, c.value, lua.LNil, false)
			r.Release = append(r.Release, c.flushSelect(op.selectOp)...)
		} else {
			u.setResult2(lua.LNil, lua.LFalse)
		}
		r.Updates = append(r.Updates, u)
		r.Release = append(r.Release, c)
		releaseChanOp(op)
	}

	if len(r.Updates) == 0 {
		ReleaseResult(r)
		return nil
	}

	if caller != nil {
		u := acquireTaskUpdate()
		u.State = caller
		r.Updates = append(r.Updates, u)
	}
	r.Yields = true
	return r
}

// handoff completes a send when receiver is waiting - both wake
func (c *Channel) handoff(sender *lua.LState, value lua.LValue, senderSel *SelectOp, recvOp *chanOp) *ChannelResult {
	defer releaseChanOp(recvOp)

	r := acquireResult()
	r.Yields = true

	// Sender result
	uSender := acquireTaskUpdate()
	uSender.State = sender
	if senderSel != nil {
		uSender.setSelectResult(sender, c.value, value, true)
	} else {
		uSender.setResult1(lua.LTrue)
	}

	// Receiver result
	uRecv := acquireTaskUpdate()
	uRecv.State = recvOp.task
	if recvOp.selectOp != nil {
		uRecv.setSelectResult(recvOp.task, c.value, value, true)
	} else {
		uRecv.setResult2(value, lua.LTrue)
	}

	// Sender first (caller of Send), so select can use updates[0]
	r.Updates = append(r.Updates, uSender)
	r.Updates = append(r.Updates, uRecv)

	r.Release = append(r.Release, c)
	r.Release = append(r.Release, c.flushSelect(senderSel)...)
	r.Release = append(r.Release, c.flushSelect(recvOp.selectOp)...)
	return r
}

// senderDone returns result when send completes without blocking (buffered)
func (c *Channel) senderDone(task *lua.LState, value lua.LValue, sel *SelectOp) *ChannelResult {
	r := acquireResult()
	u := acquireTaskUpdate()
	u.State = task
	if sel != nil {
		u.setSelectResult(task, c.value, value, true)
	} else {
		u.setResult1(lua.LTrue)
	}
	r.Updates = append(r.Updates, u)
	r.Release = append(r.Release, c.flushSelect(sel)...)
	return r
}

// blockSender blocks the sender and adds to sendq
func (c *Channel) blockSender(task *lua.LState, value lua.LValue, sel *SelectOp) *ChannelResult {
	op := acquireChanOp()
	op.isSend = true
	op.channel = c
	op.value = value
	op.task = task
	op.selectOp = sel
	c.sendq.PushBack(op)

	r := acquireResult()
	r.Yields = true
	r.Block = append(r.Block, c)
	return r
}

// receiverDone returns result when receive completes from buffer
func (c *Channel) receiverDone(task *lua.LState, value lua.LValue, sel *SelectOp) *ChannelResult {
	r := acquireResult()
	u := acquireTaskUpdate()
	u.State = task
	if sel != nil {
		u.setSelectResult(task, c.value, value, true)
	} else {
		u.setResult2(value, lua.LTrue)
	}
	r.Updates = append(r.Updates, u)
	r.Release = append(r.Release, c.flushSelect(sel)...)
	return r
}

// recvWithSenderWake returns result when receive from buffer also wakes blocked sender
func (c *Channel) recvWithSenderWake(task *lua.LState, value lua.LValue, recvSel *SelectOp, sendOp *chanOp) *ChannelResult {
	defer releaseChanOp(sendOp)

	r := acquireResult()
	r.Yields = true

	// Receiver gets buffered value
	u1 := acquireTaskUpdate()
	u1.State = task
	if recvSel != nil {
		u1.setSelectResult(task, c.value, value, true)
	} else {
		u1.setResult2(value, lua.LTrue)
	}
	r.Updates = append(r.Updates, u1)

	// Blocked sender wakes (their value is now in buffer)
	u2 := acquireTaskUpdate()
	u2.State = sendOp.task
	if sendOp.selectOp != nil {
		u2.setSelectResult(sendOp.task, c.value, sendOp.value, true)
	} else {
		u2.setResult1(lua.LTrue)
	}
	r.Updates = append(r.Updates, u2)

	r.Release = append(r.Release, c)
	r.Release = append(r.Release, c.flushSelect(recvSel)...)
	r.Release = append(r.Release, c.flushSelect(sendOp.selectOp)...)
	return r
}

// recvHandoff completes receive from blocked sender - direct handoff
func (c *Channel) recvHandoff(task *lua.LState, recvSel *SelectOp, sendOp *chanOp) *ChannelResult {
	defer releaseChanOp(sendOp)

	r := acquireResult()
	r.Yields = true

	// Receiver gets value (first, so select can use updates[0])
	u1 := acquireTaskUpdate()
	u1.State = task
	if recvSel != nil {
		u1.setSelectResult(task, c.value, sendOp.value, true)
	} else {
		u1.setResult2(sendOp.value, lua.LTrue)
	}
	r.Updates = append(r.Updates, u1)

	// Sender wakes
	u2 := acquireTaskUpdate()
	u2.State = sendOp.task
	if sendOp.selectOp != nil {
		u2.setSelectResult(sendOp.task, c.value, sendOp.value, true)
	} else {
		u2.setResult1(lua.LTrue)
	}
	r.Updates = append(r.Updates, u2)

	r.Release = append(r.Release, c)
	r.Release = append(r.Release, c.flushSelect(sendOp.selectOp)...)
	r.Release = append(r.Release, c.flushSelect(recvSel)...)
	return r
}

// receiverClosed returns result when receiving from closed channel
func (c *Channel) receiverClosed(task *lua.LState, sel *SelectOp) *ChannelResult {
	r := acquireResult()
	u := acquireTaskUpdate()
	u.State = task
	if sel != nil {
		u.setSelectResult(task, c.value, lua.LNil, false)
	} else {
		u.setResult2(lua.LNil, lua.LFalse)
	}
	r.Updates = append(r.Updates, u)
	r.Release = append(r.Release, c.flushSelect(sel)...)
	return r
}

// blockReceiver blocks the receiver and adds to recvq
func (c *Channel) blockReceiver(task *lua.LState, sel *SelectOp) *ChannelResult {
	op := acquireChanOp()
	op.isSend = false
	op.channel = c
	op.task = task
	op.selectOp = sel
	c.recvq.PushBack(op)

	r := acquireResult()
	r.Yields = true
	r.Block = append(r.Block, c)
	return r
}

// flushSelect removes all ops for a select from their channels
func (c *Channel) flushSelect(s *SelectOp) []*Channel {
	if s == nil {
		return nil
	}
	releases := make([]*Channel, 0, len(s.Cases))
	for _, op := range s.Cases {
		if op.Channel == nil {
			continue
		}
		releases = append(releases, op.Channel)
		op.Channel.discardSelect(s)
	}
	return releases
}

// discardSelect removes ops belonging to a select from this channel's queues
func (c *Channel) discardSelect(sel *SelectOp) {
	for e := c.sendq.Front(); e != nil; {
		next := e.Next()
		if op := e.Value.(*chanOp); op.selectOp != nil && op.selectOp == sel {
			c.sendq.Remove(e)
			releaseChanOp(op)
			break
		}
		e = next
	}
	for e := c.recvq.Front(); e != nil; {
		next := e.Next()
		if op := e.Value.(*chanOp); op.selectOp != nil && op.selectOp == sel {
			c.recvq.Remove(e)
			releaseChanOp(op)
			break
		}
		e = next
	}
}

func errorResult(task *lua.LState, err error) *ChannelResult {
	r := acquireResult()
	u := acquireTaskUpdate()
	u.State = task
	u.Error = err
	r.Updates = append(r.Updates, u)
	return r
}

// toInternalSelect converts public SelectOp to internal, returns nil if input is nil
func toInternalSelect(s *SelectOp) *SelectOp {
	return s // they're the same type now, but this allows future separation
}

// Object pools
var (
	chanOpPool     = sync.Pool{New: func() any { return &chanOp{} }}
	taskUpdatePool = sync.Pool{New: func() any { return &TaskUpdate{} }}
	resultPool     = sync.Pool{
		New: func() any {
			return &ChannelResult{
				Updates: make([]*TaskUpdate, 0, 4),
				Block:   make([]*Channel, 0, 2),
				Release: make([]*Channel, 0, 4),
			}
		},
	}
)

func acquireChanOp() *chanOp {
	return chanOpPool.Get().(*chanOp)
}

func releaseChanOp(op *chanOp) {
	op.isSend = false
	op.channel = nil
	op.value = nil
	op.task = nil
	op.selectOp = nil
	chanOpPool.Put(op)
}

func acquireTaskUpdate() *TaskUpdate {
	return taskUpdatePool.Get().(*TaskUpdate)
}

func releaseTaskUpdate(u *TaskUpdate) {
	u.reset()
	taskUpdatePool.Put(u)
}

func acquireResult() *ChannelResult {
	r := resultPool.Get().(*ChannelResult)
	r.reset()
	return r
}

func ReleaseResult(r *ChannelResult) {
	if r == nil {
		return
	}
	for _, u := range r.Updates {
		if u != nil {
			releaseTaskUpdate(u)
		}
	}
	resultPool.Put(r)
}
