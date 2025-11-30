package engine

import (
	"errors"
	"sync"

	lua "github.com/yuin/gopher-lua"
)

// opQueue is a slice-based queue replacing container/list to reduce allocations.
type opQueue struct {
	ops  []*ChannelOp
	head int
	tail int
	len  int
}

func newOpQueue(cap int) *opQueue {
	if cap < 4 {
		cap = 4
	}
	return &opQueue{
		ops: make([]*ChannelOp, cap),
	}
}

func (q *opQueue) Push(op *ChannelOp) {
	if q.len == len(q.ops) {
		q.grow()
	}
	q.ops[q.tail] = op
	q.tail = (q.tail + 1) % len(q.ops)
	q.len++
}

func (q *opQueue) Pop() *ChannelOp {
	if q.len == 0 {
		return nil
	}
	op := q.ops[q.head]
	q.ops[q.head] = nil
	q.head = (q.head + 1) % len(q.ops)
	q.len--
	return op
}

func (q *opQueue) Front() *ChannelOp {
	if q.len == 0 {
		return nil
	}
	return q.ops[q.head]
}

func (q *opQueue) Len() int {
	return q.len
}

func (q *opQueue) grow() {
	newCap := len(q.ops) * 2
	newOps := make([]*ChannelOp, newCap)
	if q.head < q.tail {
		copy(newOps, q.ops[q.head:q.tail])
	} else {
		n := copy(newOps, q.ops[q.head:])
		copy(newOps[n:], q.ops[:q.tail])
	}
	q.ops = newOps
	q.head = 0
	q.tail = q.len
}

func (q *opQueue) removeBySelectOp(selectOp *SelectOp) *ChannelOp {
	if q.len == 0 {
		return nil
	}
	for i := 0; i < q.len; i++ {
		idx := (q.head + i) % len(q.ops)
		if q.ops[idx].SelectOp == selectOp {
			op := q.ops[idx]
			// Shift elements to remove
			for j := i; j > 0; j-- {
				curr := (q.head + j) % len(q.ops)
				prev := (q.head + j - 1) % len(q.ops)
				q.ops[curr] = q.ops[prev]
			}
			q.ops[q.head] = nil
			q.head = (q.head + 1) % len(q.ops)
			q.len--
			return op
		}
	}
	return nil
}

// ChannelOp pool
var channelOpPool = sync.Pool{
	New: func() interface{} { return &ChannelOp{} },
}

func acquireChannelOp() *ChannelOp {
	return channelOpPool.Get().(*ChannelOp)
}

func releaseChannelOp(op *ChannelOp) {
	op.Kind = 0
	op.Channel = nil
	op.Value = nil
	op.Task = nil
	op.SelectOp = nil
	channelOpPool.Put(op)
}

// TaskUpdate pool
var taskUpdatePool = sync.Pool{
	New: func() interface{} { return &TaskUpdate{} },
}

func acquireTaskUpdate() *TaskUpdate {
	return taskUpdatePool.Get().(*TaskUpdate)
}

func releaseTaskUpdate(u *TaskUpdate) {
	u.State = nil
	u.ResultBuf[0] = nil
	u.ResultBuf[1] = nil
	u.ResultLen = 0
	u.Error = nil
	taskUpdatePool.Put(u)
}

// ChannelResult pool
var resultPool = sync.Pool{
	New: func() interface{} {
		return &ChannelResult{
			Block:   make([]*Channel, 0, 2),
			Release: make([]*Channel, 0, 2),
		}
	},
}

func acquireResult() *ChannelResult {
	r := resultPool.Get().(*ChannelResult)
	r.Yields = false
	r.UpdatesLen = 0
	r.UpdatesBuf[0] = nil
	r.UpdatesBuf[1] = nil
	r.Block = r.Block[:0]
	r.Release = r.Release[:0]
	return r
}

// ReleaseResult returns the result and its updates to the pool.
func ReleaseResult(r *ChannelResult) {
	if r == nil {
		return
	}
	// Release overflow slice updates
	if r.Updates != nil {
		for _, upd := range r.Updates {
			if upd != nil {
				releaseTaskUpdate(upd)
			}
		}
		r.Updates = nil
	} else {
		// Release inline buffer updates
		for i := 0; i < r.UpdatesLen; i++ {
			if r.UpdatesBuf[i] != nil {
				releaseTaskUpdate(r.UpdatesBuf[i])
				r.UpdatesBuf[i] = nil
			}
		}
	}
	resultPool.Put(r)
}

// Channel represents a buffered or unbuffered channel.
type Channel struct {
	mu       sync.Mutex
	name     string
	capacity int
	closed   bool
	size     int

	value     lua.LValue
	senders   *opQueue
	receivers *opQueue
}

// NewChannel creates a new channel with given buffer capacity.
func NewChannel(capacity int) *Channel {
	return &Channel{
		capacity:  capacity,
		senders:   newOpQueue(4),
		receivers: newOpQueue(4),
	}
}

// Name returns the channel name.
func (c *Channel) Name() string {
	return c.name
}

// IsClosed returns whether the channel is closed.
func (c *Channel) IsClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// Close closes the channel and wakes all blocked receivers/senders.
// The caller parameter is the LState of the task calling close.
// Returns a ChannelResult with updates for all blocked tasks and the caller.
func (c *Channel) Close(caller *lua.LState) *ChannelResult {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	r := acquireResult()

	// Wake all blocked senders with error
	for {
		op := c.senders.Pop()
		if op == nil {
			break
		}
		c.size--
		if op.Task != nil {
			u := acquireTaskUpdate()
			u.State = op.Task
			u.Error = errors.New("send on closed channel")
			r.AddUpdate(u)
			r.Release = append(r.Release, c)
		}
		releaseChannelOp(op)
	}

	// Wake all blocked receivers with (nil, false)
	for {
		op := c.receivers.Pop()
		if op == nil {
			break
		}
		if op.Task != nil {
			u := acquireTaskUpdate()
			u.State = op.Task
			u.SetResult2(lua.LNil, lua.LFalse)
			r.AddUpdate(u)
			r.Release = append(r.Release, c)
		}
		releaseChannelOp(op)
	}

	if len(r.GetUpdates()) > 0 {
		// Add update for caller to continue (close() returns nothing)
		if caller != nil {
			u := acquireTaskUpdate()
			u.State = caller
			r.AddUpdate(u)
		}
		r.Yields = true
		return r
	}

	ReleaseResult(r)
	return nil
}

// Slots returns available slots for sending.
func (c *Channel) Slots() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return (c.capacity - c.size) + c.receivers.Len()
}

// Value returns the Lua value associated with the channel.
func (c *Channel) Value() lua.LValue {
	return c.value
}

// SetValue sets the Lua value associated with the channel.
func (c *Channel) SetValue(v lua.LValue) {
	c.value = v
}

// ChannelOp represents a pending channel operation.
type ChannelOp struct {
	Kind     ChannelOpKind
	Channel  *Channel
	Value    lua.LValue
	Task     *lua.LState
	SelectOp *SelectOp
}

// ChannelOpKind indicates send or receive.
type ChannelOpKind int

const (
	SendOp ChannelOpKind = iota
	ReceiveOp
)

// SelectOp represents a select operation across multiple channels.
type SelectOp struct {
	Cases      []*ChannelOp
	HasDefault bool
	Task       *lua.LState
}

// ChannelResult represents the result of a channel operation.
// Implements lua.LValue so it can be yielded from coroutines.
type ChannelResult struct {
	Yields     bool
	UpdatesBuf [2]*TaskUpdate // Inline for common case (1-2 updates)
	UpdatesLen int
	Updates    []*TaskUpdate // Overflow for close with many blocked tasks
	Block      []*Channel
	Release    []*Channel
}

// GetUpdates returns the updates slice.
func (r *ChannelResult) GetUpdates() []*TaskUpdate {
	if r.Updates != nil {
		return r.Updates
	}
	return r.UpdatesBuf[:r.UpdatesLen]
}

// AddUpdate adds an update to the result.
func (r *ChannelResult) AddUpdate(u *TaskUpdate) {
	if r.Updates != nil {
		r.Updates = append(r.Updates, u)
		return
	}
	if r.UpdatesLen < 2 {
		r.UpdatesBuf[r.UpdatesLen] = u
		r.UpdatesLen++
		return
	}
	// Overflow: move to slice
	r.Updates = make([]*TaskUpdate, 0, 8)
	r.Updates = append(r.Updates, r.UpdatesBuf[0], r.UpdatesBuf[1], u)
}

// String implements lua.LValue.
func (r *ChannelResult) String() string {
	return "<channel_result>"
}

// Type implements lua.LValue.
func (r *ChannelResult) Type() lua.LValueType {
	return lua.LTUserData
}

// TaskUpdate represents an update to resume a task.
type TaskUpdate struct {
	State     *lua.LState
	ResultBuf [2]lua.LValue // Inline for common case (1-2 values)
	ResultLen int
	Error     error
}

// GetResult returns the result values.
func (u *TaskUpdate) GetResult() []lua.LValue {
	return u.ResultBuf[:u.ResultLen]
}

// SetResult1 sets a single result value without allocation.
func (u *TaskUpdate) SetResult1(v lua.LValue) {
	u.ResultBuf[0] = v
	u.ResultLen = 1
}

// SetResult2 sets two result values without allocation.
func (u *TaskUpdate) SetResult2(v1, v2 lua.LValue) {
	u.ResultBuf[0] = v1
	u.ResultBuf[1] = v2
	u.ResultLen = 2
}

// Send attempts to send a value on the channel.
func (c *Channel) Send(task *lua.LState, value lua.LValue, selectOp *SelectOp) *ChannelResult {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		r := acquireResult()
		u := acquireTaskUpdate()
		u.State = task
		u.Error = errors.New("send on closed channel")
		r.UpdatesBuf[0] = u
		r.UpdatesLen = 1
		return r
	}

	// Try to wake receiver first
	if recvOp := c.receivers.Pop(); recvOp != nil {
		if recvOp.Task != nil {
			r := acquireResult()
			r.Yields = true

			u1 := acquireTaskUpdate()
			u1.State = recvOp.Task
			u1.SetResult2(value, lua.LTrue)
			r.UpdatesBuf[0] = u1

			u2 := acquireTaskUpdate()
			u2.State = task
			u2.SetResult1(lua.LTrue)
			r.UpdatesBuf[1] = u2
			r.UpdatesLen = 2

			r.Release = append(r.Release, c)
			if selectOp != nil {
				r.Release = append(r.Release, c.flushSelectLocked(selectOp)...)
			}
			releaseChannelOp(recvOp)
			return r
		}
		releaseChannelOp(recvOp)
	}

	// Try buffer
	if c.size < c.capacity {
		op := acquireChannelOp()
		op.Kind = SendOp
		op.Channel = c
		op.Value = value
		c.senders.Push(op)
		c.size++

		r := acquireResult()
		u := acquireTaskUpdate()
		u.State = task
		u.SetResult1(lua.LTrue)
		r.UpdatesBuf[0] = u
		r.UpdatesLen = 1
		return r
	}

	// Block
	op := acquireChannelOp()
	op.Task = task
	op.Kind = SendOp
	op.Channel = c
	op.Value = value
	op.SelectOp = selectOp
	c.senders.Push(op)
	c.size++

	r := acquireResult()
	r.Yields = true
	r.Block = append(r.Block, c)
	return r
}

// Receive attempts to receive a value from the channel.
func (c *Channel) Receive(task *lua.LState, selectOp *SelectOp) *ChannelResult {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Try get from senders
	if sendOp := c.senders.Pop(); sendOp != nil {
		c.size--

		if sendOp.Task != nil {
			r := acquireResult()
			r.Yields = true

			u1 := acquireTaskUpdate()
			u1.State = sendOp.Task
			u1.SetResult1(lua.LTrue)
			r.UpdatesBuf[0] = u1

			u2 := acquireTaskUpdate()
			u2.State = task
			u2.SetResult2(sendOp.Value, lua.LTrue)
			r.UpdatesBuf[1] = u2
			r.UpdatesLen = 2

			r.Release = append(r.Release, c)
			releaseChannelOp(sendOp)
			return r
		}

		// Buffered value
		r := acquireResult()
		u := acquireTaskUpdate()
		u.State = task
		u.SetResult2(sendOp.Value, lua.LTrue)
		r.UpdatesBuf[0] = u
		r.UpdatesLen = 1
		releaseChannelOp(sendOp)
		return r
	}

	if c.closed {
		r := acquireResult()
		u := acquireTaskUpdate()
		u.State = task
		u.SetResult2(lua.LNil, lua.LFalse)
		r.UpdatesBuf[0] = u
		r.UpdatesLen = 1
		return r
	}

	// Block
	op := acquireChannelOp()
	op.Kind = ReceiveOp
	op.Channel = c
	op.Task = task
	op.SelectOp = selectOp
	c.receivers.Push(op)

	r := acquireResult()
	r.Yields = true
	r.Block = append(r.Block, c)
	return r
}

func (c *Channel) flushSelect(s *SelectOp) []*Channel {
	if s == nil {
		return nil
	}

	releases := make([]*Channel, 0, len(s.Cases))
	for _, caseOp := range s.Cases {
		if caseOp.Channel == nil {
			continue
		}
		releases = append(releases, caseOp.Channel)
		caseOp.Channel.discardSelect(s)
	}
	return releases
}

// flushSelectLocked is like flushSelect but caller already holds c.mu.
// Used when flushing select from within Send/Receive which already hold the lock.
func (c *Channel) flushSelectLocked(s *SelectOp) []*Channel {
	if s == nil {
		return nil
	}

	releases := make([]*Channel, 0, len(s.Cases))
	for _, caseOp := range s.Cases {
		if caseOp.Channel == nil {
			continue
		}
		releases = append(releases, caseOp.Channel)
		if caseOp.Channel == c {
			// Already locked
			caseOp.Channel.discardSelectLocked(s)
		} else {
			caseOp.Channel.discardSelect(s)
		}
	}
	return releases
}

func (c *Channel) discardSelect(selectOp *SelectOp) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.discardSelectLocked(selectOp)
}

func (c *Channel) discardSelectLocked(selectOp *SelectOp) {
	if op := c.senders.removeBySelectOp(selectOp); op != nil {
		c.size--
		releaseChannelOp(op)
	}
	if op := c.receivers.removeBySelectOp(selectOp); op != nil {
		releaseChannelOp(op)
	}
}
