package engine

import (
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

	t.Run("cleanup after close", func(t *testing.T) {
		ch := newLuaChannel(3)

		// Fill buffer
		values := []string{"first", "second", "third"}
		for _, v := range values {
			ch.send(lua.LString(v))
		}

		// Close and cleanup
		ch.closed = true
		ch.cleanup()

		// Verify cleanup
		if ch.buffer != nil {
			t.Error("buffer should be nil after cleanup")
		}
		if ch.size != 0 {
			t.Error("size should be 0 after cleanup")
		}
		if ch.read != 0 {
			t.Error("read index should be 0 after cleanup")
		}
		if ch.write != 0 {
			t.Error("write index should be 0 after cleanup")
		}
		if !ch.closed {
			t.Error("channel should remain closed after cleanup")
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
