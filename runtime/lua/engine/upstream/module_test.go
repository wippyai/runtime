package upstream

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func TestUpstreamModule(t *testing.T) {
	logger := zap.NewNop()

	t.Run("module creation and loading", func(t *testing.T) {
		ch := make(chan any, 1)
		mod := NewUpstreamModule(ch)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local upstream = require("upstream")
			assert(type(upstream) == "table")
			assert(type(upstream.send) == "function")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("send values", func(t *testing.T) {
		testCases := []struct {
			name     string
			script   string
			expected any
		}{
			{
				name: "send string",
				script: `
					local upstream = require("upstream")
					return upstream.send("hello")
				`,
				expected: "hello",
			},
			{
				name: "send number",
				script: `
					local upstream = require("upstream")
					return upstream.send(42.5)
				`,
				expected: float64(42.5),
			},
			{
				name: "send boolean",
				script: `
					local upstream = require("upstream")
					return upstream.send(true)
				`,
				expected: true,
			},
			{
				name: "send nil",
				script: `
					local upstream = require("upstream")
					return upstream.send(nil)
				`,
				expected: nil,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				ch := make(chan any, 1)
				mod := NewUpstreamModule(ch)
				vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
				require.NoError(t, err)
				defer vm.Close()

				// Run the script in a goroutine
				done := make(chan error)
				go func() {
					err := vm.DoString(context.Background(), tc.script, "test")
					done <- err
				}()

				// Wait for value with timeout
				select {
				case val := <-ch:
					// Convert Lua types to Go types for comparison
					switch v := val.(type) {
					case lua.LString:
						assert.Equal(t, tc.expected, string(v))
					case lua.LNumber:
						assert.Equal(t, tc.expected, float64(v))
					case lua.LBool:
						assert.Equal(t, tc.expected, bool(v))
					case *lua.LNilType:
						assert.Nil(t, tc.expected)
					default:
						assert.Equal(t, tc.expected, v)
					}
				case <-time.After(time.Second):
					t.Fatal("timeout waiting for value")
				}

				// Check script execution
				select {
				case err := <-done:
					assert.NoError(t, err)
				case <-time.After(time.Second):
					t.Fatal("timeout waiting for script completion")
				}
			})
		}
	})

	t.Run("channel full behavior", func(t *testing.T) {
		ch := make(chan any, 1)
		mod := NewUpstreamModule(ch)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Fill the channel
		ch <- "blocking"

		// Try to send when channel is full
		script := `
			local upstream = require("upstream")
			return upstream.send("should fail")
		`
		err = vm.DoString(context.Background(), script, "test")
		require.NoError(t, err)

		// Check that false was returned
		result := vm.State().Get(-1)
		assert.Equal(t, lua.LFalse, result)
		vm.State().Pop(1)

		// Verify channel still contains original value
		assert.Equal(t, "blocking", <-ch)
	})

	t.Run("concurrent access", func(t *testing.T) {
		ch := make(chan any, 100)
		const numGoroutines = 10

		// Spawn a WaitGroup to track goroutine completion
		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		// Spawn a slice to collect errors
		var mutex sync.Mutex
		var errors []error

		// Launch goroutines
		for i := 0; i < numGoroutines; i++ {
			go func(n int) {
				defer wg.Done()

				// Spawn a new VM instance for each goroutine
				mod := NewUpstreamModule(ch)
				vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
				if err != nil {
					mutex.Lock()
					errors = append(errors, err)
					mutex.Unlock()
					return
				}
				defer vm.Close()

				// execute the script with properly quoted string
				script := fmt.Sprintf(`
					local upstream = require("upstream")
					return upstream.send("%c")
				`, rune('A'+n))

				err = vm.DoString(context.Background(), script, "test")
				if err != nil {
					mutex.Lock()
					errors = append(errors, err)
					mutex.Unlock()
					return
				}
			}(i)
		}

		// Wait for all goroutines to complete
		wg.Wait()
		close(ch)

		// Check for any errors
		assert.Empty(t, errors, "unexpected errors during concurrent execution")

		// Collect all received values
		received := make(map[string]bool)
		for val := range ch {
			if str, ok := val.(lua.LString); ok {
				received[string(str)] = true
			}
		}

		// Verify we received all values
		assert.Equal(t, numGoroutines, len(received))
		for i := 0; i < numGoroutines; i++ {
			assert.True(t, received[string(rune('A'+i))], "missing value %c", rune('A'+i))
		}
	})
}
