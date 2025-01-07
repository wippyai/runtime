package channel

import (
	"github.com/stretchr/testify/assert"
	lua "github.com/yuin/gopher-lua"
	"testing"
)

func TestChannel_Buffer(t *testing.T) {
	t.Run("unbuffered channel creation", func(t *testing.T) {
		ch := newLuaChannel(0)
		if ch.buffer != nil {
			t.Error("unbuffered channel should have nil buffer")
		}
		if ch.capacity != 0 {
			t.Error("unbuffered channel should have 0 capacity")
		}
	})

	t.Run("buffered channel creation", func(t *testing.T) {
		capacity := 5
		ch := newLuaChannel(capacity)
		if len(ch.buffer) != capacity {
			t.Errorf("buffer length should be %d, got %d", capacity, len(ch.buffer))
		}
		if ch.capacity != capacity {
			t.Errorf("channel capacity should be %d, got %d", capacity, ch.capacity)
		}
	})

	t.Run("circular buffer operations", func(t *testing.T) {
		ch := newLuaChannel(3)

		// Fill buffer
		values := []string{"first", "second", "third"}
		for _, v := range values {
			ok := ch.send(lua.LString(v))
			if !ok {
				t.Error("send should succeed when buffer not full")
			}
		}

		// Buffer should be full
		if !ch.isFull() {
			t.Error("buffer should be full")
		}

		// Try send when full
		ok := ch.send(lua.LString("overflow"))
		if ok {
			t.Error("send should fail when buffer full")
		}

		// Read first value
		val, ok := ch.receive()
		if !ok {
			t.Error("receive should succeed when buffer has values")
		}
		if val.String() != "first" {
			t.Errorf("expected 'first', got %v", val)
		}

		// Should be able to send one more
		ok = ch.send(lua.LString("fourth"))
		if !ok {
			t.Error("send should succeed after receive")
		}

		// Verify circular buffer worked
		remaining := []string{"second", "third", "fourth"}
		for i, expected := range remaining {
			val, ok := ch.receive()
			if !ok {
				t.Errorf("receive %d should succeed", i)
			}
			if val.String() != expected {
				t.Errorf("expected '%s', got %v", expected, val)
			}
		}

		// Should be empty now
		if !ch.isEmpty() {
			t.Error("buffer should be empty")
		}
	})

	t.Run("closed channel operations", func(t *testing.T) {
		ch := newLuaChannel(3)

		// Fill partially
		ch.send(lua.LString("value"))

		// Close channel
		ch.closed = true

		// Try operations on closed channel
		ok := ch.send(lua.LString("after close"))
		if ok {
			t.Error("send should fail on closed channel")
		}

		// Should still be able to receive buffered values
		val, ok := ch.receive()
		if !ok {
			t.Error("receive should get buffered value from closed channel")
		}
		if val.String() != "value" {
			t.Errorf("wrong value received, got %v", val)
		}

		// Further receives should fail
		val, ok = ch.receive()
		if ok {
			t.Error("receive should fail when closed channel is empty")
		}
	})

	t.Run("buffer wrapping", func(t *testing.T) {
		ch := newLuaChannel(3)
		sequence := []string{
			"1", "2", "3", // Fill buffer
			"4", "5", "6", // Replace all values
			"7", "8", "9", // Replace all again
		}

		// Send values in groups of 3
		for i := 0; i < len(sequence); i += 3 {
			// Receive previous values if not first group
			if i > 0 {
				for j := 0; j < 3; j++ {
					val, ok := ch.receive()
					if !ok {
						t.Errorf("receive failed at i=%d, j=%d", i, j)
					}
					expected := sequence[i-3+j]
					if val.String() != expected {
						t.Errorf("expected '%s', got %v", expected, val)
					}
				}
			}

			// Send new values
			for j := 0; j < 3; j++ {
				ok := ch.send(lua.LString(sequence[i+j]))
				if !ok {
					t.Errorf("send failed at i=%d, j=%d", i, j)
				}
			}
		}

		// Verify final values
		for i := 6; i < 9; i++ {
			val, ok := ch.receive()
			if !ok {
				t.Errorf("final receive failed at i=%d", i)
			}
			if val.String() != sequence[i] {
				t.Errorf("expected '%s', got %v", sequence[i], val)
			}
		}

		// Buffer should be empty with indices wrapped around
		if !ch.isEmpty() {
			t.Error("buffer should be empty")
		}
		if ch.read != ch.write {
			t.Error("read and write indices should be equal when empty")
		}
	})

	t.Run("verify fixed capacity", func(t *testing.T) {
		capacity := 2
		ch := newLuaChannel(capacity)

		// Fill to capacity
		if !ch.send(lua.LString("1")) {
			t.Error("first send should succeed")
		}
		if !ch.send(lua.LString("2")) {
			t.Error("second send should succeed")
		}

		// Try to exceed capacity
		if ch.send(lua.LString("3")) {
			t.Error("send should fail when buffer at capacity")
		}

		// Verify size hasn't grown
		if ch.size != capacity {
			t.Errorf("buffer size should be %d, got %d", capacity, ch.size)
		}
		if len(ch.buffer) != capacity {
			t.Errorf("underlying buffer should remain size %d, got %d", capacity, len(ch.buffer))
		}
	})
}

func TestChannel_Operations(t *testing.T) {
	t.Run("cleanup releases all references", func(t *testing.T) {
		ch := newLuaChannel(3)

		// Fill buffer
		ch.send(lua.LString("1"))
		ch.send(lua.LString("2"))
		ch.send(lua.LString("3"))

		// Cleanup
		ch.cleanup()

		if ch.buffer != nil {
			t.Error("cleanup should set buffer to nil")
		}
		if ch.size != 0 {
			t.Error("cleanup should reset size")
		}
		if ch.read != 0 {
			t.Error("cleanup should reset read index")
		}
		if ch.write != 0 {
			t.Error("cleanup should reset write index")
		}
	})

	t.Run("buffer state checks", func(t *testing.T) {
		ch := newLuaChannel(2)

		if !ch.isEmpty() {
			t.Error("new channel should be empty")
		}
		if ch.isFull() {
			t.Error("new channel should not be full")
		}

		ch.send(lua.LString("1"))
		if ch.isEmpty() {
			t.Error("channel with one item should not be empty")
		}
		if ch.isFull() {
			t.Error("channel with one item in capacity 2 should not be full")
		}

		ch.send(lua.LString("2"))
		if !ch.isFull() {
			t.Error("channel at capacity should be full")
		}
	})

	t.Run("circular buffer full cycle", func(t *testing.T) {
		ch := newLuaChannel(2)

		// Fill
		if !ch.send(lua.LString("1")) {
			t.Error("first send failed")
		}
		if !ch.send(lua.LString("2")) {
			t.Error("second send failed")
		}

		// Should be full
		if !ch.isFull() {
			t.Error("channel should be full")
		}

		// Read one
		val, ok := ch.receive()
		if !ok || val.String() != "1" {
			t.Error("receive failed or wrong value")
		}

		// Write one wrapping around
		if !ch.send(lua.LString("3")) {
			t.Error("send after receive failed")
		}

		// Read all
		remaining := []string{"2", "3"}
		for i, expected := range remaining {
			val, ok := ch.receive()
			if !ok {
				t.Errorf("receive %d failed", i)
			}
			if val.String() != expected {
				t.Errorf("expected %s, got %s", expected, val.String())
			}
		}

		// Should be empty
		if !ch.isEmpty() {
			t.Error("channel should be empty after reading all")
		}
	})

	t.Run("closed channel behavior", func(t *testing.T) {
		ch := newLuaChannel(2)

		// Fill partially and close
		ch.send(lua.LString("1"))
		ch.closed = true

		// Send should fail
		if ch.send(lua.LString("2")) {
			t.Error("send on closed channel should fail")
		}

		// Should be able to receive buffered value
		val, ok := ch.receive()
		if !ok || val.String() != "1" {
			t.Error("should receive buffered value from closed channel")
		}

		// Further receives should fail
		val, ok = ch.receive()
		if ok || val != nil {
			t.Error("receive from empty closed channel should fail")
		}

		// Cleanup after close
		if ch.isEmpty() {
			ch.cleanup()
			if ch.buffer != nil || ch.size != 0 {
				t.Error("cleanup after close should clear state")
			}
		}
	})

	t.Run("zero capacity channel", func(t *testing.T) {
		ch := newLuaChannel(0)

		if ch.buffer != nil {
			t.Error("zero capacity channel should have nil buffer")
		}

		if !ch.isEmpty() {
			t.Error("zero capacity channel should be empty")
		}

		if !ch.isFull() {
			t.Error("zero capacity channel should always be full")
		}

		// Operations should fail
		if ch.send(lua.LString("test")) {
			t.Error("send on zero capacity channel should fail")
		}

		val, ok := ch.receive()
		if ok || val != nil {
			t.Error("receive from zero capacity channel should fail")
		}
	})

	t.Run("edge case index wrapping", func(t *testing.T) {
		ch := newLuaChannel(3)

		// Multiple cycles of filling and emptying
		for cycle := 0; cycle < 3; cycle++ {
			// Fill
			for i := 0; i < 3; i++ {
				ok := ch.send(lua.LString(string(rune('A' + i))))
				if !ok {
					t.Errorf("send failed on cycle %d, item %d", cycle, i)
				}
			}

			// Empty
			for i := 0; i < 3; i++ {
				val, ok := ch.receive()
				if !ok {
					t.Errorf("receive failed on cycle %d, item %d", cycle, i)
				}
				expected := string(rune('A' + i))
				if val.String() != expected {
					t.Errorf("wrong value on cycle %d, item %d: expected %s, got %s",
						cycle, i, expected, val.String())
				}
			}
		}

		// Verify indices wrapped correctly
		if ch.read != ch.write {
			t.Error("indices should be equal after complete cycles")
		}
	})
}

func TestChanOperation(t *testing.T) {
	t.Run("String representation", func(t *testing.T) {
		testCases := []struct {
			name     string
			op       *chanOperation
			expected string
		}{
			{
				name: "send operation",
				op: &chanOperation{
					opType: chanSend,
					ch:     newLuaChannel(0),
					value:  lua.LString("test value"),
				},
				expected: "channel.send{value=test value}",
			},
			{
				name: "receive operation",
				op: &chanOperation{
					opType: chanReceive,
					ch:     newLuaChannel(0),
				},
				expected: "channel.receive",
			},
			{
				name: "close operation",
				op: &chanOperation{
					opType: chanClose,
					ch:     newLuaChannel(0),
				},
				expected: "channel.close",
			},
			{
				name: "invalid operation type",
				op: &chanOperation{
					opType: chanOp(999), // Invalid operation type
					ch:     newLuaChannel(0),
				},
				expected: "unknown",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				assert.Equal(t, tc.expected, tc.op.String())
			})
		}

		// Test different value types in send operation
		valueTests := []struct {
			name     string
			value    lua.LValue
			expected string
		}{
			{
				name:     "string value",
				value:    lua.LString("hello"),
				expected: "channel.send{value=hello}",
			},
			{
				name:     "number value",
				value:    lua.LNumber(42),
				expected: "channel.send{value=42}",
			},
			{
				name:     "bool value",
				value:    lua.LBool(true),
				expected: "channel.send{value=true}",
			},
			{
				name:     "nil value",
				value:    lua.LNil,
				expected: "channel.send{value=nil}",
			},
			{
				name:     "table value",
				value:    &lua.LTable{},
				expected: "channel.send{value=table: ", // Just check prefix as table string contains address
			},
		}

		for _, vt := range valueTests {
			t.Run(vt.name, func(t *testing.T) {
				op := &chanOperation{
					opType: chanSend,
					ch:     newLuaChannel(0),
					value:  vt.value,
				}
				if vt.name == "table value" {
					assert.Contains(t, op.String(), vt.expected)
				} else {
					assert.Equal(t, vt.expected, op.String())
				}
			})
		}
	})

	t.Run("Type method", func(t *testing.T) {
		ops := []*chanOperation{
			{opType: chanSend, ch: newLuaChannel(0), value: lua.LString("test")},
			{opType: chanReceive, ch: newLuaChannel(0)},
			{opType: chanClose, ch: newLuaChannel(0)},
		}

		for _, op := range ops {
			assert.Equal(t, lua.LTUserData, op.Type(), "chanOperation should always return LTUserData type")
		}
	})

	t.Run("Operation with nil channel", func(t *testing.T) {
		op := &chanOperation{
			opType: chanSend,
			ch:     nil,
			value:  lua.LString("test"),
		}
		assert.NotPanics(t, func() {
			_ = op.String()
			_ = op.Type()
		}, "Operations should handle nil channel gracefully")
	})

	t.Run("Operation with nil value", func(t *testing.T) {
		op := &chanOperation{
			opType: chanSend,
			ch:     newLuaChannel(0),
			value:  nil,
		}
		assert.NotPanics(t, func() {
			str := op.String()
			assert.Contains(t, str, "channel.send{value=<nil>}")
		}, "Send operation should handle nil value gracefully")
	})
}
