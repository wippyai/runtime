package channel

import (
	"github.com/stretchr/testify/assert"
	lua "github.com/yuin/gopher-lua"
	"testing"
)

func TestChannel(t *testing.T) {
	t.Run("creation and initialization", func(t *testing.T) {
		tests := []struct {
			name        string
			capacity    int
			expectBuf   bool
			expectEmpty bool
			expectFull  bool
		}{
			{
				name:        "zero capacity channel",
				capacity:    0,
				expectBuf:   false,
				expectEmpty: true,
				expectFull:  true,
			},
			{
				name:        "capacity one channel",
				capacity:    1,
				expectBuf:   true,
				expectEmpty: true,
				expectFull:  false,
			},
			{
				name:        "larger capacity channel",
				capacity:    5,
				expectBuf:   true,
				expectEmpty: true,
				expectFull:  false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				ch := newChannel(tt.capacity)

				if tt.expectBuf && ch.buffer == nil {
					t.Error("expected non-nil buffer")
				}
				if !tt.expectBuf && ch.buffer != nil {
					t.Error("expected nil buffer")
				}
				assert.Equal(t, tt.capacity, ch.capacity)
				assert.Equal(t, tt.expectEmpty, ch.isEmpty())
				assert.Equal(t, tt.expectFull, ch.isFull())
				assert.Equal(t, 0, ch.size)
			})
		}
	})

	t.Run("named channel operations", func(t *testing.T) {
		t.Run("creation", func(t *testing.T) {
			ch := Named("test-channel", 5)
			assert.Equal(t, "test-channel", ch.Name())
			assert.True(t, ch.IsNamed())
			assert.Equal(t, 5, ch.capacity)
		})

		t.Run("anonymous channel", func(t *testing.T) {
			ch := newChannel(5)
			assert.Empty(t, ch.Name())
			assert.False(t, ch.IsNamed())
		})
	})

	t.Run("buffer operations", func(t *testing.T) {
		t.Run("sequential operations", func(t *testing.T) {
			ch := newChannel(3)

			// Test sequential send operations
			assert.True(t, ch.send(lua.LString("first")))
			assert.Equal(t, 1, ch.size)
			assert.True(t, ch.send(lua.LString("second")))
			assert.Equal(t, 2, ch.size)
			assert.True(t, ch.send(lua.LString("third")))
			assert.Equal(t, 3, ch.size)
			assert.True(t, ch.isFull())

			// Test sequential receive operations
			val, ok := ch.receive()
			assert.True(t, ok)
			assert.Equal(t, "first", val.String())
			assert.Equal(t, 2, ch.size)

			val, ok = ch.receive()
			assert.True(t, ok)
			assert.Equal(t, "second", val.String())
			assert.Equal(t, 1, ch.size)

			val, ok = ch.receive()
			assert.True(t, ok)
			assert.Equal(t, "third", val.String())
			assert.Equal(t, 0, ch.size)
			assert.True(t, ch.isEmpty())
		})

		t.Run("circular buffer wraparound", func(t *testing.T) {
			ch := newChannel(2)

			// Fill buffer
			assert.True(t, ch.send(lua.LString("1")))
			assert.True(t, ch.send(lua.LString("2")))

			// Remove one and add another to cause wraparound
			val, ok := ch.receive()
			assert.True(t, ok)
			assert.Equal(t, "1", val.String())

			assert.True(t, ch.send(lua.LString("3")))

			// Verify contents after wraparound
			val, ok = ch.receive()
			assert.True(t, ok)
			assert.Equal(t, "2", val.String())

			val, ok = ch.receive()
			assert.True(t, ok)
			assert.Equal(t, "3", val.String())
		})
	})

	t.Run("channel closure", func(t *testing.T) {
		t.Run("operations on closed channel", func(t *testing.T) {
			ch := newChannel(2)

			// Fill and close
			assert.True(t, ch.send(lua.LString("value")))
			ch.closed = true

			// Test operations after closure
			assert.False(t, ch.send(lua.LString("new")))

			// Should still receive buffered values
			val, ok := ch.receive()
			assert.True(t, ok)
			assert.Equal(t, "value", val.String())

			// Further receives should fail
			val, ok = ch.receive()
			assert.False(t, ok)
			assert.Nil(t, val)
		})

		t.Run("cleanup after closure", func(t *testing.T) {
			ch := newChannel(3)
			ch.send(lua.LString("1"))
			ch.send(lua.LString("2"))

			ch.cleanup()

			assert.Nil(t, ch.buffer)
			assert.Equal(t, 0, ch.size)
			assert.Equal(t, 0, ch.read)
			assert.Equal(t, 0, ch.write)
			assert.True(t, ch.closed)

			// Operations after cleanup should fail
			assert.False(t, ch.send(lua.LString("new")))
			val, ok := ch.receive()
			assert.False(t, ok)
			assert.Nil(t, val)
		})
	})

	t.Run("stress testing", func(t *testing.T) {
		t.Run("rapid send/receive cycles", func(t *testing.T) {
			ch := newChannel(3)
			cycles := 100

			for i := 0; i < cycles; i++ {
				// Fill buffer
				for j := 0; j < 3; j++ {
					assert.True(t, ch.send(lua.LNumber(i*3+j)))
				}

				// Empty buffer
				for j := 0; j < 3; j++ {
					val, ok := ch.receive()
					assert.True(t, ok)
					assert.Equal(t, float64(i*3+j), float64(val.(lua.LNumber)))
				}

				assert.True(t, ch.isEmpty())
				assert.Equal(t, ch.read, ch.write)
			}
		})

		t.Run("alternating operations", func(t *testing.T) {
			ch := newChannel(2)

			for i := 0; i < 50; i++ {
				assert.True(t, ch.send(lua.LNumber(i)))
				val, ok := ch.receive()
				assert.True(t, ok)
				assert.Equal(t, float64(i), float64(val.(lua.LNumber)))
				assert.True(t, ch.isEmpty())
			}
		})
	})

	t.Run("error conditions", func(t *testing.T) {
		t.Run("operations on full channel", func(t *testing.T) {
			ch := newChannel(1)

			assert.True(t, ch.send(lua.LString("value")))
			assert.False(t, ch.send(lua.LString("overflow")))
			assert.True(t, ch.isFull())
		})

		t.Run("operations on empty channel", func(t *testing.T) {
			ch := newChannel(1)

			val, ok := ch.receive()
			assert.False(t, ok)
			assert.Nil(t, val)
			assert.True(t, ch.isEmpty())
		})
	})

	t.Run("additional edge cases", func(t *testing.T) {
		t.Run("send after receive with exact capacity", func(t *testing.T) {
			ch := newChannel(1)
			assert.True(t, ch.send(lua.LString("first")))
			val, ok := ch.receive()
			assert.True(t, ok)
			assert.Equal(t, "first", val.String())
			assert.True(t, ch.send(lua.LString("second"))) // Should succeed exactly at capacity
		})

		t.Run("zero-capacity channel edge cases", func(t *testing.T) {
			ch := newChannel(0)
			assert.True(t, ch.isFull())  // Zero capacity channels are always full
			assert.True(t, ch.isEmpty()) // And always empty
			assert.False(t, ch.send(lua.LString("test")))
			val, ok := ch.receive()
			assert.False(t, ok)
			assert.Nil(t, val)
		})

		t.Run("cleanup with partial buffer", func(t *testing.T) {
			ch := newChannel(3)
			assert.True(t, ch.send(lua.LString("1")))
			ch.receive()                              // Read one value
			assert.True(t, ch.send(lua.LString("2"))) // Write one more

			// Now have a partial buffer with gaps
			ch.cleanup()
			assert.Nil(t, ch.buffer)
			assert.True(t, ch.closed)
			assert.Equal(t, 0, ch.size)
		})

		t.Run("rapid close and cleanup cycle", func(t *testing.T) {
			ch := newChannel(2)
			for i := 0; i < 100; i++ {
				ch.closed = true
				ch.cleanup()
				ch = newChannel(2) // Create new channel
				assert.True(t, ch.send(lua.LString("test")))
			}
		})

		t.Run("boundary indices", func(t *testing.T) {
			ch := newChannel(3)
			// Fill completely
			for i := 0; i < ch.capacity; i++ {
				assert.True(t, ch.send(lua.LString("value")))
			}
			// Empty completely
			for i := 0; i < ch.capacity; i++ {
				_, ok := ch.receive()
				assert.True(t, ok)
			}
			// Indices should wrap exactly to 0
			assert.Equal(t, 0, ch.read%ch.capacity)
			assert.Equal(t, 0, ch.write%ch.capacity)
		})

		t.Run("mixed value types", func(t *testing.T) {
			ch := newChannel(4)
			values := []lua.LValue{
				lua.LString("string"),
				lua.LNumber(42),
				lua.LBool(true),
				lua.LNil,
			}

			// Send mixed types
			for _, v := range values {
				assert.True(t, ch.send(v))
			}

			// Receive and verify
			for _, expected := range values {
				val, ok := ch.receive()
				assert.True(t, ok)
				assert.Equal(t, expected.Type(), val.Type())
			}
		})
	})
}

// Benchmarks
func BenchmarkChannel(b *testing.B) {
	b.Run("send/receive cycle", func(b *testing.B) {
		ch := newChannel(1)
		value := lua.LString("test")
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			ch.send(value)
			ch.receive()
		}
	})

	b.Run("buffer fill and drain", func(b *testing.B) {
		ch := newChannel(100)
		value := lua.LString("test")
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			// Fill buffer
			for j := 0; j < ch.capacity; j++ {
				ch.send(value)
			}
			// Drain buffer
			for j := 0; j < ch.capacity; j++ {
				ch.receive()
			}
		}
	})

	b.Run("circular buffer wraparound", func(b *testing.B) {
		ch := newChannel(3)
		value := lua.LString("test")
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			// Force wraparound pattern
			ch.send(value)
			ch.send(value)
			ch.receive()
			ch.send(value)
			ch.receive()
			ch.receive()
		}
	})

	b.Run("concurrent size small buffer", func(b *testing.B) {
		ch := newChannel(10)
		value := lua.LString("test")
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			ch.send(value)
			ch.size++ // Simulate concurrent size updates
			ch.receive()
			ch.size-- // Simulate concurrent size updates
		}
	})

	b.Run("zero capacity operations", func(b *testing.B) {
		ch := newChannel(0)
		value := lua.LString("test")
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			ch.send(value) // Will always fail
			ch.receive()   // Will always fail
		}
	})

	b.Run("cleanup overhead", func(b *testing.B) {
		value := lua.LString("test")
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			ch := newChannel(100)
			// Fill partially
			for j := 0; j < 50; j++ {
				ch.send(value)
			}
			ch.cleanup()
		}
	})
}
