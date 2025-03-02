// module_test.go
package upstream

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func setupTest(t *testing.T, ch chan<- payload.Payload) (*engine.VM, context.Context) {
	logger := zap.NewNop()
	mod := NewUpstreamModule()

	vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
	require.NoError(t, err)

	// Create context with upstream channel
	ctx := context.WithValue(context.Background(), Ctx, ch)

	return vm, ctx
}

func TestUpstreamModule(t *testing.T) {
	t.Run("module creation and loading", func(t *testing.T) {
		ch := make(chan payload.Payload, 1)
		vm, ctx := setupTest(t, ch)
		defer vm.Close()

		err := vm.DoString(ctx, `
			local upstream = require("upstream")
			if type(upstream) ~= "table" then
				error("expected upstream to be a table")
			end
			if type(upstream.send) ~= "function" then
				error("expected upstream.send to be a function")
			end
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("send values", func(t *testing.T) {
		testCases := []struct {
			name     string
			script   string
			validate func(*testing.T, payload.Payload)
		}{
			{
				name: "send string",
				script: `
					local upstream = require("upstream")
					local ok, err = upstream.send("hello")
					if not ok then
						error("failed to send: " .. tostring(err))
					end
					return ok, err
				`,
				validate: func(t *testing.T, p payload.Payload) {
					assert.Equal(t, payload.Lua, p.Format())
					assert.Equal(t, lua.LString("hello"), p.Data())
				},
			},
			{
				name: "send number",
				script: `
					local upstream = require("upstream")
					local ok, err = upstream.send(42.5)
					if not ok then
						error("failed to send: " .. tostring(err))
					end
					return ok, err
				`,
				validate: func(t *testing.T, p payload.Payload) {
					assert.Equal(t, payload.Lua, p.Format())
					assert.Equal(t, lua.LNumber(42.5), p.Data())
				},
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				ch := make(chan payload.Payload, 1)
				vm, ctx := setupTest(t, ch)

				// Create synchronization channels
				done := make(chan struct{})
				errCh := make(chan error, 1)

				// Run the script in a goroutine
				go func() {
					defer close(done)
					defer vm.Close()

					if err := vm.DoString(ctx, tc.script, "test"); err != nil {
						errCh <- err
						return
					}
				}()

				// Wait for value or timeout
				select {
				case p := <-ch:
					tc.validate(t, p)
				case err := <-errCh:
					t.Fatal("script error:", err)
				case <-time.After(time.Second):
					t.Fatal("timeout waiting for value")
				}

				// Wait for script completion
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Fatal("timeout waiting for script completion")
				}
			})
		}
	})

	t.Run("channel full behavior", func(t *testing.T) {
		ch := make(chan payload.Payload, 1)
		vm, ctx := setupTest(t, ch)
		defer vm.Close()

		// Fill the channel
		ch <- payload.NewPayload("blocking", payload.String)

		script := `
			local upstream = require("upstream")
			local ok, err = upstream.send("should fail")
			if ok then
				error("expected send to fail")
			end
			if err ~= "channel full" then
				error("expected 'channel full' error, got: " .. tostring(err))
			end
			return ok, err
		`
		err := vm.DoString(ctx, script, "test")
		require.NoError(t, err)

		// Verify channel still contains original value
		p := <-ch
		assert.Equal(t, "blocking", p.Data())
	})

	t.Run("concurrent access", func(t *testing.T) {
		ch := make(chan payload.Payload, 100)
		const numGoroutines = 10
		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		// Create a WaitGroup to track VM closures
		var closeWg sync.WaitGroup
		closeWg.Add(numGoroutines)

		// Track errors using a channel
		errCh := make(chan error, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func(n int) {
				defer wg.Done()

				vm, ctx := setupTest(t, ch)

				// Ensure VM is closed after all operations
				defer func() {
					vm.Close()
					closeWg.Done()
				}()

				script := fmt.Sprintf(`
					local upstream = require("upstream")
					local ok, err = upstream.send("%c")
					if not ok then
						error("failed to send: " .. tostring(err))
					end
					return ok, err
				`, rune('A'+n))

				if err := vm.DoString(ctx, script, "test"); err != nil {
					errCh <- fmt.Errorf("goroutine %d error: %v", n, err)
				}
			}(i)
		}

		// Wait for all operations to complete
		wg.Wait()
		close(ch)

		// Wait for all VMs to close
		closeWg.Wait()

		// Check for any errors
		close(errCh)
		var errors []error
		for err := range errCh {
			errors = append(errors, err)
		}
		assert.Empty(t, errors, "unexpected errors during concurrent execution")

		// Collect and verify received values
		received := make(map[string]bool)
		for p := range ch {
			if str, ok := p.Data().(lua.LString); ok {
				received[string(str)] = true
			}
		}

		assert.Equal(t, numGoroutines, len(received), "expected %d unique values", numGoroutines)
		for i := 0; i < numGoroutines; i++ {
			assert.True(t, received[string(rune('A'+i))], "missing value %c", rune('A'+i))
		}
	})

	t.Run("missing context", func(t *testing.T) {
		logger := zap.NewNop()
		mod := NewUpstreamModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		script := `
			local upstream = require("upstream")
			local ok, err = upstream.send("test")
			if ok then
				error("expected send to fail")
			end
			if err ~= "no upstream channel found in context" then
				error("expected 'no upstream channel found in context' error, got: " .. tostring(err))
			end
			return ok, err
		`
		err = vm.DoString(context.Background(), script, "test")
		require.NoError(t, err)

		// Verify the returned values
		ok := vm.State().Get(-2)
		errMsg := vm.State().Get(-1)
		vm.State().Pop(2)

		assert.Equal(t, lua.LFalse, ok)
		assert.Equal(t, lua.LString("no upstream channel found in context"), errMsg)
	})
}
