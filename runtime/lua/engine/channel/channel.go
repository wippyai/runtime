package channel

import (
	"fmt"
	lua "github.com/yuin/gopher-lua"
)

type chanOp int

const (
	chanSend chanOp = iota
	chanReceive
	chanClose
)

// chanOperation sent via yields to coordinate channel communication
type chanOperation struct {
	opType chanOp
	ch     *Channel
	value  lua.LValue
}

func (y *chanOperation) String() string {
	switch y.opType {
	case chanSend:
		return fmt.Sprintf("channel.send{value=%+v}", y.value)
	case chanReceive:
		return fmt.Sprintf("channel.receive")
	case chanClose:
		return fmt.Sprintf("channel.close")
	}
	return "unknown"
}

func (y *chanOperation) Type() lua.LValueType {
	return lua.LTUserData
}

// Channel represents a buffered or unbuffered channel, this  is NOT thread safe, external synchronization is required
type Channel struct {
	buffer   []lua.LValue // Buffer for values
	capacity int          // Buffer capacity (0 for unbuffered)
	closed   bool         // Whether channel is closed
	read     int          // Read index for buffer
	write    int          // Write index for buffer
	size     int          // Current number of items in buffer
	external string       // External channel name
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

func newExternalChannel(name string) *Channel { // todo: move to external package?
	return &Channel{
		external: name,
		capacity: 0,
	}
}

func (c *Channel) ExternalName() string {
	return c.external
}

func (c *Channel) IsExternal() bool {
	return c.external != ""
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
	if c.closed || c.isFull() {
		return false
	}

	c.buffer[c.write] = value
	c.write = (c.write + 1) % c.capacity
	c.size++
	return true
}

func (c *Channel) receive() (lua.LValue, bool) {
	if c.isEmpty() {
		return nil, false
	}

	value := c.buffer[c.read]
	c.buffer[c.read] = nil // Clear reference
	c.read = (c.read + 1) % c.capacity
	c.size--
	return value, true
}
