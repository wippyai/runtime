package engine

import (
	"errors"
	"fmt"
	"sync"

	lua "github.com/yuin/gopher-lua"
)

// ChannelOpKind indicates send or receive operation type.
type ChannelOpKind int

const (
	SendOp ChannelOpKind = iota
	ReceiveOp
)

// ChannelOp represents a pending channel operation.
type ChannelOp struct {
	Kind     ChannelOpKind
	Channel  *Channel
	Value    lua.LValue
	Task     *lua.LState
	SelectOp *SelectOp
}

// SelectOp represents a select operation across multiple channels.
type SelectOp struct {
	Cases      []*ChannelOp
	HasDefault bool
	Task       *lua.LState
}

// TaskUpdate represents an update to resume a blocked task.
type TaskUpdate struct {
	State     *lua.LState
	Error     error
	resultBuf [3]lua.LValue
	resultLen int
}

// GetResult returns the result values to pass back to Lua.
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

// ChannelResult represents the result of a channel operation.
type ChannelResult struct {
	Yields     bool
	Block      []*Channel
	Release    []*Channel
	updatesBuf [2]*TaskUpdate
	updatesLen int
	updates    []*TaskUpdate
}

func (r *ChannelResult) String() string       { return "<channel_result>" }
func (r *ChannelResult) Type() lua.LValueType { return lua.LTUserData }

// GetUpdates returns all task updates from this result.
func (r *ChannelResult) GetUpdates() []*TaskUpdate {
	if r.updates != nil {
		return r.updates
	}
	return r.updatesBuf[:r.updatesLen]
}

func (r *ChannelResult) addUpdate(u *TaskUpdate) {
	if r.updates != nil {
		r.updates = append(r.updates, u)
		return
	}
	if r.updatesLen < 2 {
		r.updatesBuf[r.updatesLen] = u
		r.updatesLen++
		return
	}
	r.updates = make([]*TaskUpdate, 0, 8)
	r.updates = append(r.updates, r.updatesBuf[0], r.updatesBuf[1], u)
}

func (r *ChannelResult) reset() {
	r.Yields = false
	r.updatesLen = 0
	r.updatesBuf[0] = nil
	r.updatesBuf[1] = nil
	r.updates = nil
	r.Block = r.Block[:0]
	r.Release = r.Release[:0]
}

// Channel represents a Go-like channel with optional buffering.
type Channel struct {
	mu        sync.Mutex
	capacity  int
	size      int
	closed    bool
	value     lua.LValue
	senders   *opQueue
	receivers *opQueue
}

// NewChannel creates a channel with the given buffer capacity.
func NewChannel(capacity int) *Channel {
	if capacity < 0 {
		capacity = 0
	}
	return &Channel{
		capacity:  capacity,
		senders:   newOpQueue(4),
		receivers: newOpQueue(4),
	}
}

func (c *Channel) Value() lua.LValue     { return c.value }
func (c *Channel) SetValue(v lua.LValue) { c.value = v }

func (c *Channel) IsClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// Size returns the current number of items in the buffer.
func (c *Channel) Size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.size
}

// Slots returns available send slots (buffer space + waiting receivers).
func (c *Channel) Slots() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return (c.capacity - c.size) + c.receivers.Len()
}

// CanSend returns true if a send would not block.
func (c *Channel) CanSend() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.receivers.Len() > 0 || (!c.closed && c.size < c.capacity)
}

// CanReceive returns true if a receive would not block.
func (c *Channel) CanReceive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.senders.Len() > 0
}

// Send attempts to send a value. Returns result indicating if operation completed or must block.
func (c *Channel) Send(task *lua.LState, value lua.LValue, selectOp *SelectOp) *ChannelResult {
	c.mu.Lock()
	defer c.mu.Unlock()

	fmt.Printf("[CHANNEL Send] closed=%v, receivers.Len=%d, size=%d, capacity=%d\n", c.closed, c.receivers.Len(), c.size, c.capacity)

	if c.closed {
		return c.errorResult(task, errors.New("send on closed channel"))
	}

	// Try direct handoff to waiting receiver
	if recvOp := c.receivers.Pop(); recvOp != nil {
		fmt.Printf("[CHANNEL Send] found waiting receiver, doing direct handoff\n")
		return c.completeSendToReceiver(task, value, selectOp, recvOp)
	}

	// Try buffering
	if c.size < c.capacity {
		fmt.Printf("[CHANNEL Send] buffering (size=%d < capacity=%d)\n", c.size, c.capacity)
		return c.bufferSend(task, value, selectOp)
	}

	// Must block
	fmt.Printf("[CHANNEL Send] must block\n")
	return c.blockSend(task, value, selectOp)
}

// Receive attempts to receive a value. Returns result indicating if operation completed or must block.
func (c *Channel) Receive(task *lua.LState, selectOp *SelectOp) *ChannelResult {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Try to get value from sender
	if sendOp := c.senders.Pop(); sendOp != nil {
		// Only decrement size for buffered sends (Task == nil means it was buffered)
		if sendOp.Task == nil {
			c.size--
			// After consuming buffered value, check if blocked sender can now proceed
			if c.size < c.capacity {
				if nextSend := c.senders.Peek(); nextSend != nil && nextSend.Task != nil {
					// There's a blocked sender that can now buffer their value
					c.senders.Pop()
					unblockedTask := nextSend.Task
					nextSend.Task = nil // Mark as buffered (no longer blocking)
					c.size++
					// Push the now-buffered value back to senders queue
					c.senders.Push(nextSend)
					// Return result that wakes both original receiver and the now-unblocked sender
					return c.completeReceiveAndUnblockSender(task, selectOp, sendOp, nextSend, unblockedTask)
				}
			}
		}
		return c.completeReceiveFromSender(task, selectOp, sendOp)
	}

	// Channel empty
	if c.closed {
		return c.closedReceiveResult(task, selectOp)
	}

	// Must block
	return c.blockReceive(task, selectOp)
}

// Close closes the channel, waking all blocked operations.
func (c *Channel) Close(caller *lua.LState) *ChannelResult {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	r := acquireResult()

	// Error blocked senders but keep buffered values
	c.closeBlockedSenders(r)

	// Wake blocked receivers with (nil, false)
	c.wakeBlockedReceivers(r)

	if len(r.GetUpdates()) == 0 {
		ReleaseResult(r)
		return nil
	}

	if caller != nil {
		u := acquireTaskUpdate()
		u.State = caller
		r.addUpdate(u)
	}
	r.Yields = true
	return r
}

func (c *Channel) errorResult(task *lua.LState, err error) *ChannelResult {
	r := acquireResult()
	u := acquireTaskUpdate()
	u.State = task
	u.Error = err
	r.updatesBuf[0] = u
	r.updatesLen = 1
	return r
}

func (c *Channel) completeSendToReceiver(sender *lua.LState, value lua.LValue, senderSelect *SelectOp, recvOp *ChannelOp) *ChannelResult {
	defer releaseChannelOp(recvOp)

	if recvOp.Task == nil {
		return nil
	}

	r := acquireResult()
	r.Yields = true

	// Receiver update
	u1 := acquireTaskUpdate()
	u1.State = recvOp.Task
	if recvOp.SelectOp != nil {
		u1.setSelectResult(recvOp.Task, c.value, value, true)
	} else {
		u1.setResult2(value, lua.LTrue)
	}
	r.updatesBuf[0] = u1

	// Sender update
	u2 := acquireTaskUpdate()
	u2.State = sender
	if senderSelect != nil && sender != nil {
		u2.setSelectResult(sender, c.value, nil, true)
	} else {
		u2.setResult1(lua.LTrue)
	}
	r.updatesBuf[1] = u2
	r.updatesLen = 2

	r.Release = append(r.Release, c)
	r.Release = append(r.Release, c.flushSelectLocked(senderSelect)...)
	r.Release = append(r.Release, c.flushSelectLocked(recvOp.SelectOp)...)

	return r
}

func (c *Channel) bufferSend(task *lua.LState, value lua.LValue, selectOp *SelectOp) *ChannelResult {
	op := acquireChannelOp()
	op.Kind = SendOp
	op.Channel = c
	op.Value = value
	c.senders.Push(op)
	c.size++

	r := acquireResult()
	u := acquireTaskUpdate()
	u.State = task
	if selectOp != nil && task != nil {
		u.setSelectResult(task, c.value, value, true)
	} else {
		u.setResult2(value, lua.LTrue)
	}
	r.updatesBuf[0] = u
	r.updatesLen = 1
	return r
}

func (c *Channel) blockSend(task *lua.LState, value lua.LValue, selectOp *SelectOp) *ChannelResult {
	op := acquireChannelOp()
	op.Kind = SendOp
	op.Channel = c
	op.Value = value
	op.Task = task
	op.SelectOp = selectOp
	c.senders.Push(op)
	// Don't increment size for blocked senders - size represents actual buffered values

	r := acquireResult()
	r.Yields = true
	r.Block = append(r.Block, c)
	return r
}

func (c *Channel) completeReceiveFromSender(receiver *lua.LState, receiverSelect *SelectOp, sendOp *ChannelOp) *ChannelResult {
	defer releaseChannelOp(sendOp)

	value := sendOp.Value

	// Buffered value (no blocked sender)
	if sendOp.Task == nil {
		r := acquireResult()
		u := acquireTaskUpdate()
		u.State = receiver
		if receiverSelect != nil && receiver != nil {
			u.setSelectResult(receiver, c.value, value, true)
		} else {
			u.setResult2(value, lua.LTrue)
		}
		r.updatesBuf[0] = u
		r.updatesLen = 1
		return r
	}

	// Blocked sender
	r := acquireResult()
	r.Yields = true

	// Sender update
	u1 := acquireTaskUpdate()
	u1.State = sendOp.Task
	if sendOp.SelectOp != nil {
		u1.setSelectResult(sendOp.Task, c.value, value, true)
	} else {
		u1.setResult1(lua.LTrue)
	}
	r.updatesBuf[0] = u1

	// Receiver update
	u2 := acquireTaskUpdate()
	u2.State = receiver
	if receiverSelect != nil && receiver != nil {
		u2.setSelectResult(receiver, c.value, value, true)
	} else {
		u2.setResult2(value, lua.LTrue)
	}
	r.updatesBuf[1] = u2
	r.updatesLen = 2

	r.Release = append(r.Release, c)
	r.Release = append(r.Release, c.flushSelectLocked(sendOp.SelectOp)...)
	r.Release = append(r.Release, c.flushSelectLocked(receiverSelect)...)

	return r
}

// completeReceiveAndUnblockSender handles the case where receiving a buffered value
// frees space for a blocked sender to buffer their value.
func (c *Channel) completeReceiveAndUnblockSender(receiver *lua.LState, receiverSelect *SelectOp, bufferedOp, nowBufferedOp *ChannelOp, unblockedSenderTask *lua.LState) *ChannelResult {
	defer releaseChannelOp(bufferedOp)
	// Don't release nowBufferedOp - it's still in the senders queue as a buffered value

	r := acquireResult()
	r.Yields = true

	// Receiver gets the buffered value
	u1 := acquireTaskUpdate()
	u1.State = receiver
	if receiverSelect != nil && receiver != nil {
		u1.setSelectResult(receiver, c.value, bufferedOp.Value, true)
	} else {
		u1.setResult2(bufferedOp.Value, lua.LTrue)
	}
	r.updatesBuf[0] = u1

	// Previously blocked sender now succeeded (their value is now buffered)
	u2 := acquireTaskUpdate()
	u2.State = unblockedSenderTask
	if nowBufferedOp.SelectOp != nil {
		u2.setSelectResult(unblockedSenderTask, c.value, nowBufferedOp.Value, true)
	} else {
		u2.setResult1(lua.LTrue)
	}
	r.updatesBuf[1] = u2
	r.updatesLen = 2

	r.Release = append(r.Release, c)
	r.Release = append(r.Release, c.flushSelectLocked(nowBufferedOp.SelectOp)...)
	r.Release = append(r.Release, c.flushSelectLocked(receiverSelect)...)

	return r
}

func (c *Channel) closedReceiveResult(task *lua.LState, selectOp *SelectOp) *ChannelResult {
	r := acquireResult()
	u := acquireTaskUpdate()
	u.State = task
	if selectOp != nil && task != nil {
		u.setSelectResult(task, c.value, lua.LNil, false)
	} else {
		u.setResult2(lua.LNil, lua.LFalse)
	}
	r.updatesBuf[0] = u
	r.updatesLen = 1
	return r
}

func (c *Channel) blockReceive(task *lua.LState, selectOp *SelectOp) *ChannelResult {
	fmt.Printf("[CHANNEL blockReceive] task blocking on channel, receivers will be %d\n", c.receivers.Len()+1)
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

func (c *Channel) closeBlockedSenders(r *ChannelResult) {
	var buffered []*ChannelOp
	for {
		op := c.senders.Pop()
		if op == nil {
			break
		}
		if op.Task != nil {
			// Blocked senders don't have size incremented, no decrement needed
			u := acquireTaskUpdate()
			u.State = op.Task
			u.Error = errors.New("send on closed channel")
			r.addUpdate(u)
			r.Release = append(r.Release, c)
			r.Release = append(r.Release, c.flushSelectLocked(op.SelectOp)...)
			releaseChannelOp(op)
		} else {
			buffered = append(buffered, op)
		}
	}
	for _, op := range buffered {
		c.senders.Push(op)
	}
}

func (c *Channel) wakeBlockedReceivers(r *ChannelResult) {
	for {
		op := c.receivers.Pop()
		if op == nil {
			break
		}
		if op.Task != nil {
			u := acquireTaskUpdate()
			u.State = op.Task
			if op.SelectOp != nil {
				u.setSelectResult(op.Task, c.value, lua.LNil, false)
				r.Release = append(r.Release, c.flushSelectLocked(op.SelectOp)...)
			} else {
				u.setResult2(lua.LNil, lua.LFalse)
			}
			r.addUpdate(u)
			r.Release = append(r.Release, c)
		}
		releaseChannelOp(op)
	}
}

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

// opQueue is a circular buffer queue for channel operations.
type opQueue struct {
	ops  []*ChannelOp
	head int
	tail int
	len  int
}

func newOpQueue(capacity int) *opQueue {
	if capacity < 4 {
		capacity = 4
	}
	return &opQueue{ops: make([]*ChannelOp, capacity)}
}

func (q *opQueue) Len() int { return q.len }

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

func (q *opQueue) Peek() *ChannelOp {
	if q.len == 0 {
		return nil
	}
	return q.ops[q.head]
}

func (q *opQueue) grow() {
	newOps := make([]*ChannelOp, len(q.ops)*2)
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
	for i := 0; i < q.len; i++ {
		idx := (q.head + i) % len(q.ops)
		if q.ops[idx].SelectOp == selectOp {
			op := q.ops[idx]
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

// Object pools to reduce allocations
var (
	channelOpPool  = sync.Pool{New: func() interface{} { return &ChannelOp{} }}
	taskUpdatePool = sync.Pool{New: func() interface{} { return &TaskUpdate{} }}
	resultPool     = sync.Pool{
		New: func() interface{} {
			return &ChannelResult{
				Block:   make([]*Channel, 0, 2),
				Release: make([]*Channel, 0, 2),
			}
		},
	}
)

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

// ReleaseResult returns a result and its updates to the pool.
func ReleaseResult(r *ChannelResult) {
	if r == nil {
		return
	}
	if r.updates != nil {
		for _, u := range r.updates {
			if u != nil {
				releaseTaskUpdate(u)
			}
		}
	} else {
		for i := 0; i < r.updatesLen; i++ {
			if r.updatesBuf[i] != nil {
				releaseTaskUpdate(r.updatesBuf[i])
			}
		}
	}
	resultPool.Put(r)
}
