package events

import (
	"context"
	ctxapi "github.com/wippyai/runtime/api/context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/channel"
	"github.com/wippyai/runtime/runtime/lua/engine/coroutine"
	"github.com/wippyai/runtime/system/eventbus"
	luaconv "github.com/wippyai/runtime/system/payload/lua"
	luaapi "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func newTestContext() context.Context {
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	return ctx
}

func TestEventsModule_BasicSubscribe(t *testing.T) {
	logger := zap.NewNop()

	// Create a real event bus
	bus := eventbus.NewBus()

	// Create our events module
	mod := NewEventsModule(logger)

	ready := make(chan struct{})

	// Create VM with needed modules
	vm, err := engine.NewCVM(
		logger,
		engine.WithLoader(mod.Name(), mod.Loader),
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		engine.WithGlobalFunction("mark_ready", func(_ *luaapi.LState) int {
			close(ready)
			return 0
		}),
	)
	require.NoError(t, err)
	defer vm.Close()

	// Import self-contained test script
	err = vm.Import(`
		function test_subscribe()
			local events = require("events")
			local received_events = {}
			local done_ch = channel.new(0)

			-- Create subscription
			local subscription = events.subscribe("test-system", "test-kind.*")
			assert(subscription ~= nil, "Subscription should not be nil")
			
			-- Get channel
			local ch = subscription:channel()
			assert(ch ~= nil, "Channel should not be nil")

			mark_ready()

			-- Process events in background
			coroutine.spawn(function()
				local event, ok = ch:receive()

				if ok then
					-- Store the event
					table.insert(received_events, event)
					
					-- Verify event fields
					assert(event.system == "test-system", "Wrong system: " .. tostring(event.system))
					assert(event.kind == "test-kind.created", "Wrong kind: " .. tostring(event.kind))
					assert(event.path == "test/path", "Wrong path: " .. tostring(event.path))
					assert(event.data == "test data", "Wrong data: " .. tostring(event.data))
					
					-- Signal completion
					done_ch:send(true)
				end
			end)
						
			-- Wait for completion
			done_ch:receive()
			
			-- Clean up
			subscription:close()
			
			-- Return test results
			return {success = true, events=received_events, event_count = #received_events}	
		end
	`, "test", "test_subscribe")
	require.NoError(t, err)

	// Create runner with coroutine layer
	wrapped := engine.NewRunner(
		vm,
		engine.WithLayer(channel.NewChannelLayer()),
		engine.WithLayer(coroutine.NewCoroutineLayer()),
	)

	ctx := event.WithBus(newTestContext(), bus)

	go func() {
		<-ready
		bus.Send(ctx, event.Event{
			System: "test-system",
			Kind:   "test-kind.created",
			Path:   "test/path",
			Data:   "test data",
		})
	}()

	// Start execution but wait for the ready signal
	result, err := wrapped.Execute(ctx, "test_subscribe")
	require.NoError(t, err)
	require.NotNil(t, result)

	res := luaconv.ToGoAny(result)

	mp, ok := res.(map[string]any)
	require.True(t, ok)

	// assert that the test was successful
	require.True(t, mp["success"].(bool))
	require.Equal(t, float64(1), mp["event_count"].(float64))
}

func TestEventsModule_SendAndReceive(t *testing.T) {
	logger := zap.NewNop()

	// Create a real event bus
	bus := eventbus.NewBus()

	// Create our events module
	mod := NewEventsModule(logger)

	ready := make(chan struct{})

	// Create VM with needed modules
	vm, err := engine.NewCVM(
		logger,
		engine.WithLoader(mod.Name(), mod.Loader),
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		engine.WithGlobalFunction("mark_ready", func(_ *luaapi.LState) int {
			close(ready)
			return 0
		}),
	)
	require.NoError(t, err)
	defer vm.Close()

	// Import test script that tests send and receive
	err = vm.Import(`
		function test_send_receive()
			local events = require("events")
			local received_events = {}
			local done_ch = channel.new(0)

			-- Create subscription first
			local subscription = events.subscribe("test-system", "*")
			assert(subscription ~= nil, "Subscription should not be nil")
			
			-- Get channel
			local ch = subscription:channel()
			assert(ch ~= nil, "Channel should not be nil")

			-- Process events in background
			coroutine.spawn(function()
				local event, ok = ch:receive()
				if ok then
					table.insert(received_events, event)
					done_ch:send(true)
				end
			end)

			mark_ready()

			-- Send an event from Lua
			local success = events.send("test-system", "user.created", "users/456", {
				user_id = 456,
				name = "John Doe",
				email = "john@example.com"
			})
			
			assert(success == true, "Send should return true")
			
			-- Wait for the event to be received
			done_ch:receive()
			
			-- Verify the received event
			assert(#received_events == 1, "Should have received exactly one event")
			local evt = received_events[1]
			
			assert(evt.system == "test-system", "Wrong system: " .. tostring(evt.system))
			assert(evt.kind == "user.created", "Wrong kind: " .. tostring(evt.kind))
			assert(evt.path == "users/456", "Wrong path: " .. tostring(evt.path))
			assert(type(evt.data) == "table", "Data should be a table")
			assert(evt.data.user_id == 456, "Wrong user_id: " .. tostring(evt.data.user_id))
			assert(evt.data.name == "John Doe", "Wrong name: " .. tostring(evt.data.name))
			assert(evt.data.email == "john@example.com", "Wrong email: " .. tostring(evt.data.email))
			
			-- Clean up
			subscription:close()
			
			return {success = true, event_count = #received_events}
		end
	`, "test", "test_send_receive")
	require.NoError(t, err)

	// Create runner with coroutine layer
	wrapped := engine.NewRunner(
		vm,
		engine.WithLayer(channel.NewChannelLayer()),
		engine.WithLayer(coroutine.NewCoroutineLayer()),
	)

	ctx := event.WithBus(newTestContext(), bus)

	// Wait for ready signal before starting
	go func() {
		<-ready
	}()

	// Execute the test
	result, err := wrapped.Execute(ctx, "test_send_receive")
	require.NoError(t, err)
	require.NotNil(t, result)

	res := luaconv.ToGoAny(result)

	mp, ok := res.(map[string]any)
	require.True(t, ok)

	// Assert that the test was successful
	require.True(t, mp["success"].(bool))
	require.Equal(t, float64(1), mp["event_count"].(float64))
}

func TestEventsModule_SendWithoutData(t *testing.T) {
	logger := zap.NewNop()

	// Create a real event bus
	bus := eventbus.NewBus()

	// Create our events module
	mod := NewEventsModule(logger)

	// Create VM with needed modules
	vm, err := engine.NewCVM(
		logger,
		engine.WithLoader(mod.Name(), mod.Loader),
	)
	require.NoError(t, err)
	defer vm.Close()

	// Import simple send test
	err = vm.Import(`
		function test_send_no_data()
			local events = require("events")
			
			-- Send event without data
			local success = events.send("test-system", "ping", "test/ping")
			
			return {success = success}
		end
	`, "test", "test_send_no_data")
	require.NoError(t, err)

	// Create simple runner
	wrapped := engine.NewRunner(vm)

	ctx := event.WithBus(newTestContext(), bus)

	// Execute the test
	result, err := wrapped.Execute(ctx, "test_send_no_data")
	require.NoError(t, err)
	require.NotNil(t, result)

	res := luaconv.ToGoAny(result)

	mp, ok := res.(map[string]any)
	require.True(t, ok)

	// Assert that send was successful
	require.True(t, mp["success"].(bool))
}
