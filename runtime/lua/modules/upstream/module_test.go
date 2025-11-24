package upstream

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/channel"
	"github.com/wippyai/runtime/runtime/lua/engine/coroutine"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// channelSender wraps a channel as an UpstreamSender for tests
type channelSender struct {
	ch chan<- payload.Payload
}

func (s *channelSender) Send(p payload.Payload) error {
	select {
	case s.ch <- p:
		return nil
	default:
		return errors.New("upstream channel full")
	}
}

func setupTest(t *testing.T, ch chan<- payload.Payload) (*engine.VM, context.Context) {
	logger := zap.NewNop()
	mod := NewUpstreamModule()

	vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
	require.NoError(t, err)

	// Create context with upstream sender in FrameContext
	ctx, fc := ctxapi.OpenFrameContext(ctxapi.NewRootContext())
	sender := &channelSender{ch: ch}
	err = fc.Set(runtime.UpstreamSenderKey, sender)
	require.NoError(t, err)

	return vm, ctx
}

func setupTestWithRunner(t *testing.T, ch chan<- payload.Payload, upstream runtime.Upstream) (*engine.CoroutineVM, context.Context) {
	logger := zap.NewNop()
	mod := NewUpstreamModule()

	// Add channel module for tests
	channelMod := channel.NewChannelModule()

	cvm, err := engine.NewCVM(logger,
		engine.WithLoader(mod.Name(), mod.Loader),
		engine.WithLoader(channelMod.Name(), channelMod.Loader))
	require.NoError(t, err)

	// Create context with upstream sender and Upstream handler in FrameContext
	ctx, fc := ctxapi.OpenFrameContext(ctxapi.NewRootContext())
	if ch != nil {
		sender := &channelSender{ch: ch}
		err = fc.Set(runtime.UpstreamSenderKey, sender)
		require.NoError(t, err)
	}
	if upstream != nil {
		err = fc.Set(runtime.UpstreamHandlerKey, upstream)
		require.NoError(t, err)
	}

	// Add transcoder for task tests
	ctx = payload.WithTranscoder(ctx, &testTranscoder{})

	return cvm, ctx
}

// testTranscoder is a minimal transcoder for testing
type testTranscoder struct{}

func (t *testTranscoder) Transcode(p payload.Payload, format payload.Format) (payload.Payload, error) {
	if p.Format() == format {
		return p, nil
	}

	switch format {
	case payload.Lua:
		data := p.Data()
		var luaVal lua.LValue
		switch v := data.(type) {
		case nil:
			luaVal = lua.LNil
		case string:
			luaVal = lua.LString(v)
		case lua.LString:
			luaVal = v
		case int:
			luaVal = lua.LNumber(v)
		case float64:
			luaVal = lua.LNumber(v)
		case bool:
			luaVal = lua.LBool(v)
		default:
			luaVal = lua.LNil
		}
		return payload.NewPayload(luaVal, payload.Lua), nil
	case payload.Golang:
		data := p.Data()
		if lval, ok := data.(lua.LValue); ok {
			switch v := lval.(type) {
			case lua.LString:
				return payload.NewPayload(string(v), payload.Golang), nil
			case lua.LNumber:
				return payload.NewPayload(float64(v), payload.Golang), nil
			case lua.LBool:
				return payload.NewPayload(bool(v), payload.Golang), nil
			case *lua.LNilType:
				return payload.NewPayload(nil, payload.Golang), nil
			}
		}
		return p, nil
	default:
		return p, nil
	}
}

func (t *testTranscoder) Unmarshal(p payload.Payload, v interface{}) error {
	// Not needed for these tests
	return nil
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
			if type(upstream.request) ~= "function" then
				error("expected upstream.request to be a function")
			end
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("send fire-and-forget values", func(t *testing.T) {
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

				done := make(chan struct{})
				errCh := make(chan error, 1)

				go func() {
					defer close(done)
					defer vm.Close()

					if err := vm.DoString(ctx, tc.script, "test"); err != nil {
						errCh <- err
						return
					}
				}()

				select {
				case p := <-ch:
					tc.validate(t, p)
				case err := <-errCh:
					t.Fatal("script error:", err)
				case <-time.After(time.Second):
					t.Fatal("timeout waiting for value")
				}

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
			if err ~= "upstream channel full" then
				error("expected 'upstream channel full' error, got: " .. tostring(err))
			end
			return ok, err
		`
		err := vm.DoString(ctx, script, "test")
		require.NoError(t, err)

		// Verify channel still contains original value
		p := <-ch
		assert.Equal(t, "blocking", p.Data())
	})
}

// mockUpstream implements Upstream interface for testing
type mockUpstream struct {
	mu       sync.Mutex
	requests []runtime.Command
}

func newMockUpstream() *mockUpstream {
	return &mockUpstream{
		requests: make([]runtime.Command, 0),
	}
}

func (m *mockUpstream) SendRequest(cmd runtime.Command) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, cmd)
	return nil
}

func (m *mockUpstream) FlushRequests() []runtime.Command {
	m.mu.Lock()
	defer m.mu.Unlock()
	reqs := m.requests
	m.requests = make([]runtime.Command, 0)
	return reqs
}

func (m *mockUpstream) GetRequests() []runtime.Command {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]runtime.Command{}, m.requests...)
}

func TestUpstreamRequest(t *testing.T) {
	t.Run("create request", func(t *testing.T) {
		upstream := newMockUpstream()
		cvm, ctx := setupTestWithRunner(t, nil, upstream)
		defer cvm.Close()

		runner := engine.NewRunner(cvm,
			engine.WithLayer(coroutine.NewCoroutineLayer()),
			engine.WithLayer(channel.NewChannelLayer()))

		uw, frameCtx := runner.InitUnitOfWork(ctx)
		defer uw.Close()

		script := `
			local upstream = require("upstream")
			local req = upstream.request("timer.sleep", 5000)
			if not req then
				error("failed to create request")
			end
			return req
		`

		err := cvm.State().DoString(script)
		require.NoError(t, err)

		// Get the returned request
		val := cvm.State().Get(-1)
		cvm.State().Pop(1)

		ud, ok := val.(*lua.LUserData)
		require.True(t, ok, "expected userdata")

		req, ok := ud.Value.(*Request)
		require.True(t, ok, "expected Request")

		assert.Equal(t, "timer.sleep", string(req.Type()))
		assert.Equal(t, 1, len(req.Params()))

		_ = frameCtx
	})

	t.Run("send request and get response", func(t *testing.T) {
		upstream := newMockUpstream()
		cvm, ctx := setupTestWithRunner(t, nil, upstream)
		defer cvm.Close()

		runner := engine.NewRunner(cvm,
			engine.WithLayer(coroutine.NewCoroutineLayer()),
			engine.WithLayer(channel.NewChannelLayer()))

		uw, frameCtx := runner.InitUnitOfWork(ctx)
		defer uw.Close()

		script := `
			local channel = require("channel")
			local upstream = require("upstream")

			local req = upstream.request("timer.sleep", 5000)
			local ok, err = upstream.send(req)
			if not ok then
				error("failed to send request: " .. tostring(err))
			end

			local ch = req:response()
			return ch
		`

		err := cvm.State().DoString(script)
		require.NoError(t, err)

		// Get the response channel (returned from script)
		_ = cvm.State().Get(-1) // chVal
		cvm.State().Pop(1)

		// Verify request was queued
		requests := upstream.GetRequests()
		require.Equal(t, 1, len(requests))
		assert.Equal(t, "timer.sleep", string(requests[0].Type()))

		// Complete the request
		result := payload.NewPayload(true, payload.Golang)
		err = requests[0].Complete(&runtime.Result{Value: result, Error: nil})
		require.NoError(t, err)

		// Verify request completed (type assert to access Request-specific methods)
		req, ok := requests[0].(*Request)
		require.True(t, ok)
		assert.True(t, req.IsCompleted())

		_ = frameCtx
	})
}

func TestUpstreamTask(t *testing.T) {
	t.Run("task input and complete", func(t *testing.T) {
		ch := make(chan payload.Payload, 1)
		cvm, ctx := setupTestWithRunner(t, ch, nil)
		defer cvm.Close()

		runner := engine.NewRunner(cvm,
			engine.WithLayer(coroutine.NewCoroutineLayer()),
			engine.WithLayer(channel.NewChannelLayer()))

		uw, frameCtx := runner.InitUnitOfWork(ctx)
		defer uw.Close()

		// Create task
		resultCh := make(chan runtime.Result, 1)
		task := NewTask(
			payload.NewPayload(lua.LString("test input"), payload.Lua),
			func(result runtime.Result) {
				resultCh <- result
			},
		)

		// Wrap task and set as global
		taskVal := WrapTask(cvm.State(), task)
		cvm.State().SetGlobal("test_task", taskVal)

		script := `
			local input = test_task:input()
			if input ~= "test input" then
				error("expected 'test input', got: " .. tostring(input))
			end

			local ok = test_task:complete("completed")
			if not ok then
				error("failed to complete task")
			end
		`

		err := cvm.State().DoString(script)
		require.NoError(t, err)

		// Wait for completion callback
		select {
		case result := <-resultCh:
			assert.NoError(t, result.Error)
			luaVal, ok := result.Value.Data().(lua.LString)
			require.True(t, ok)
			assert.Equal(t, "completed", string(luaVal))
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for task completion")
		}

		_ = frameCtx
	})

	t.Run("task fail", func(t *testing.T) {
		ch := make(chan payload.Payload, 1)
		cvm, ctx := setupTestWithRunner(t, ch, nil)
		defer cvm.Close()

		runner := engine.NewRunner(cvm,
			engine.WithLayer(coroutine.NewCoroutineLayer()),
			engine.WithLayer(channel.NewChannelLayer()))

		uw, frameCtx := runner.InitUnitOfWork(ctx)
		defer uw.Close()

		// Create task
		resultCh := make(chan runtime.Result, 1)
		task := NewTask(
			payload.NewPayload(lua.LString("test input"), payload.Lua),
			func(result runtime.Result) {
				resultCh <- result
			},
		)

		// Wrap task and set as global
		taskVal := WrapTask(cvm.State(), task)
		cvm.State().SetGlobal("test_task", taskVal)

		script := `
			local ok = test_task:fail("something went wrong")
			if not ok then
				error("failed to fail task")
			end
		`

		err := cvm.State().DoString(script)
		require.NoError(t, err)

		// Wait for completion callback
		select {
		case result := <-resultCh:
			require.Error(t, result.Error)
			assert.Equal(t, "something went wrong", result.Error.Error())
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for task failure")
		}

		_ = frameCtx
	})
}

func TestConcurrentAccess(t *testing.T) {
	ch := make(chan payload.Payload, 100)
	const numGoroutines = 10
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	var closeWg sync.WaitGroup
	closeWg.Add(numGoroutines)

	errCh := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(n int) {
			defer wg.Done()

			vm, ctx := setupTest(t, ch)

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
				errCh <- fmt.Errorf("goroutine %d error: %w", n, err)
			}
		}(i)
	}

	wg.Wait()
	close(ch)
	closeWg.Wait()

	close(errCh)
	var errors []error
	for err := range errCh {
		errors = append(errors, err)
	}
	assert.Empty(t, errors, "unexpected errors during concurrent execution")

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
}
