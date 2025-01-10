package channel

import (
	lua "github.com/yuin/gopher-lua"
)

// Channel represents a buffered or unbuffered channel.
// This is NOT thread-safe; inbox synchronization is required.
type Channel struct {
	buffer   []lua.LValue
	name     string
	capacity int
	closed   bool
	read     int
	write    int
	size     int
}

// Named creates a named channel intended for external use.
// Named channels are unbuffered.
func Named(name string, capacity int) *Channel {
	return &Channel{name: name, capacity: capacity}
}

// newChannel creates a new channel with the given capacity.
// A capacity of 0 creates an unbuffered channel.
func newChannel(capacity int) *Channel {
	var buf []lua.LValue
	if capacity > 0 {
		buf = make([]lua.LValue, capacity)
	}

	return &Channel{buffer: buf, capacity: capacity}
}

// Name returns the external name of the channel, if any.
func (c *Channel) Name() string {
	return c.name
}

// IsNamed returns true if the channel has an external name.
func (c *Channel) IsNamed() bool {
	return c.name != ""
}

// cleanup releases all references and resets internal state.
func (c *Channel) cleanup() {
	for i := range c.buffer {
		c.buffer[i] = nil
	}
	c.buffer = nil
	c.size = 0
	c.read = 0
	c.write = 0
	c.closed = true // Keep closed = true after cleanup as original code
}

// isFull returns true if the channel's buffer is full.
func (c *Channel) isFull() bool {
	return c.size >= c.capacity
}

// isEmpty returns true if the channel's buffer is empty.
func (c *Channel) isEmpty() bool {
	return c.size == 0
}

// send sends a value to the channel.
// Returns false if the channel is closed or full.
func (c *Channel) send(value lua.LValue) bool {
	if c.closed || c.isFull() {
		return false
	}

	if c.capacity > 0 {
		if c.isFull() {
			return false
		}
		c.buffer[c.write] = value
		c.write = (c.write + 1) % c.capacity
		c.size++
	}

	return true
}

// receive receives a value from the channel.
// Returns the value and true if successful, or nil and false if the channel is empty.
func (c *Channel) receive() (lua.LValue, bool) {
	if c.capacity > 0 && c.isEmpty() {
		return nil, false
	}

	if c.capacity == 0 {
		return nil, false
	}

	value := c.buffer[c.read]
	c.buffer[c.read] = nil
	c.read = (c.read + 1) % c.capacity
	c.size--
	return value, true
}
