package engine

import (
	"fmt"
	lua "github.com/yuin/gopher-lua"
	"log"
)

type chanOp int

const (
	chanOpSend chanOp = iota
	chanOpReceive
	chanOpClose
)

// ChanOperation sent via yields to coordinate channel communication
type ChanOperation struct {
	opType chanOp
	ch     *Channel
	value  lua.LValue
}

func (y *ChanOperation) String() string {
	switch y.opType {
	case chanOpSend:
		return fmt.Sprintf("channel_send{value=%+v}", y.value)
	case chanOpReceive:
		return fmt.Sprintf("channel_receive")
	case chanOpClose:
		return fmt.Sprintf("channel_close")
	}
	return "unknown"
}

func (y *ChanOperation) Type() lua.LValueType {
	return lua.LTUserData
}

// Channel represents a buffered or unbuffered channel
type Channel struct {
	buffer   []lua.LValue // Buffer for values
	capacity int          // Buffer capacity (0 for unbuffered)
	closed   bool         // Whether channel is closed
	read     int          // Read index for buffer
	write    int          // Write index for buffer
	size     int          // Current number of items in buffer
}

func newLuaChannel(capacity int) *Channel {
	var buf []lua.LValue
	if capacity > 0 {
		buf = make([]lua.LValue, capacity)
	}
	return &Channel{
		buffer:   buf,
		capacity: capacity,
	}
}

// Cleanup releases all references and resets internal state
func (c *Channel) cleanup() {
	// Clear buffer and release references
	for i := range c.buffer {
		c.buffer[i] = nil
	}
	c.buffer = nil
	c.size = 0
	c.read = 0
	c.write = 0
	// Keep closed = true
}

// Buffer operations
func (c *Channel) isFull() bool {
	return c.size >= c.capacity
}

func (c *Channel) isEmpty() bool {
	return c.size == 0
}

func (c *Channel) send(value lua.LValue) bool {
	//log.Printf("DEBUG: Channel send attempt - size=%d capacity=%d closed=%v", c.size, c.capacity, c.closed)

	if c.closed || c.isFull() {
		return false
	}

	c.buffer[c.write] = value
	c.write = (c.write + 1) % c.capacity
	c.size++
	return true
}

func (c *Channel) receive() (lua.LValue, bool) {
	//log.Printf("DEBUG: Channel receive attempt - size=%d capacity=%d closed=%v", c.size, c.capacity, c.closed)
	if c.isEmpty() {
		return nil, false
	}

	value := c.buffer[c.read]
	c.buffer[c.read] = nil // Clear reference
	c.read = (c.read + 1) % c.capacity
	c.size--
	return value, true
}

func (e *CoroutineVM) bindChannels() {
	L := e.vm.state

	// Create metatable for channel userdata
	mt := L.NewTypeMetatable("channel")
	L.SetField(mt, "__index", mt)

	// Global channel table
	channelLib := L.NewTable()
	L.SetGlobal("channel", channelLib)

	// Constructor
	L.SetField(channelLib, "new", L.NewFunction(func(L *lua.LState) int {
		capacity := L.OptInt(1, 0)
		if capacity < 0 {
			L.RaiseError("channel capacity must be >= 0")
			return 0
		}

		ch := newLuaChannel(capacity)
		ud := L.NewUserData()
		ud.Value = ch
		L.SetMetatable(ud, mt)
		L.Push(ud)
		return 1
	}))

	// Send method
	L.SetField(mt, "send", L.NewFunction(func(L *lua.LState) int {
		ch := L.CheckUserData(1).Value.(*Channel)
		value := L.CheckAny(2)

		if ch.closed {
			L.RaiseError("attempt to send on closed channel")
			return 1
		}

		// For buffered channels, try to send immediately
		if ch.capacity > 0 && !ch.isFull() {
			ok := ch.send(value)
			L.Push(lua.LBool(ok))
			return 1
		}

		log.Printf("DEBUG: Creating send operation for channel")

		// Create and yield the operation
		ret := L.Yield(&ChanOperation{
			opType: chanOpSend,
			ch:     ch,
			value:  value,
		})
		log.Printf("DEBUG: Send operation resumed with: %v", ret)
		return -1
	}))

	// Receive method
	L.SetField(mt, "receive", L.NewFunction(func(L *lua.LState) int {
		ch := L.CheckUserData(1).Value.(*Channel)

		// Try to receive immediately first
		if value, ok := ch.receive(); ok {
			L.Push(value)
			L.Push(lua.LBool(true))
			return 2
		}

		// Channel is empty and closed
		if ch.closed {
			L.Push(lua.LNil)
			L.Push(lua.LBool(false))
			return 2
		}

		log.Printf("DEBUG: Creating receive operation for channel")

		// Create and yield the operation
		ret := L.Yield(&ChanOperation{
			opType: chanOpReceive,
			ch:     ch,
		})
		log.Printf("DEBUG: Receive operation resumed with: %v", ret)
		return -1
	}))

	// Close method
	L.SetField(mt, "close", L.NewFunction(func(L *lua.LState) int {
		ch := L.CheckUserData(1).Value.(*Channel)

		if ch.closed {
			L.RaiseError("attempt to close already closed channel")
			return 0
		}

		ret := L.Yield(&ChanOperation{
			opType: chanOpClose,
			ch:     ch,
		})
		log.Printf("DEBUG: Close operation completed with: %v", ret)
		return 0
	}))
}
