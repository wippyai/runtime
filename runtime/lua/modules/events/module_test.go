package events

import (
	"context"
	"testing"

	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"github.com/ponyruntime/pony/system/eventbus"
	luaconv "github.com/ponyruntime/pony/system/payload/lua"
	"github.com/stretchr/testify/require"
	luaapi "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

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

	ctx := event.WithBus(context.Background(), bus)

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
