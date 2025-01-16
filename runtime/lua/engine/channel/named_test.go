package channel

import (
	"context"
	"strings"
	"testing"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func TestNamedChannelVisibility(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	vm.SetContext(context.Background())

	err = vm.StartString(`
		-- Create two named channels
		local ch1 = channel.named("channel1", 1)
		local ch2 = channel.named("channel2", 1)

		-- Only block on channel1
		local val = ch1:receive()

		coroutine.yield("blocked")
	`, "test")
	assert.NoError(t, err)

	runtime := NewChannelRunner()
	tasks, err := runtime.Step(vm)
	assert.NoError(t, err)
	assert.Equal(t, 0, len(tasks), "expected no tasks")

	// Check open channels after first step
	channels := runtime.GetOpenChannels()
	assert.Equal(t, 1, len(channels), "expected exactly one visible channel")
	assert.Equal(t, "channel1", channels[0].Name, "expected channel1 to be visible")
}

func TestNamedChannelSelectVisibility(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	vm.SetContext(context.Background())

	err = vm.StartString(`
		-- Create named channels with different capacities
		local ch1 = channel.named("select_ch1", 0) -- unbuffered
		local ch2 = channel.named("select_ch2", 1) -- buffered
		local done = channel.new(0) -- regular channel for coordination

		-- Start select operation that will block on both named channels
		coroutine.spawn(function()
			local result = channel.select{
				ch1:case_receive(),
				ch2:case_receive()
			}
			done:send("completed:" .. result.value)
			coroutine.yield("select_completed")
		end)

		coroutine.yield("select_started")

		-- Send value through runtime to ch1
		coroutine.yield("ready_for_send")

		-- Wait for completion
		local msg = done:receive()
		coroutine.yield("done")
		coroutine.yield(msg)
	`, "test")
	assert.NoError(t, err)

	runtime := NewChannelRunner()
	tasks, err := runtime.Step(vm)
	assert.NoError(t, err)

	var yields []string
	checkChannels := func(expectedNames []string) {
		channels := runtime.GetOpenChannels()
		assert.Equal(t, len(expectedNames), len(channels), "unexpected number of open channels")

		actualNames := make(map[string]bool)
		for _, ch := range channels {
			actualNames[ch.Name] = true
		}

		for _, expected := range expectedNames {
			assert.True(t, actualNames[expected], "expected channel %s to be visible", expected)
		}
	}

	// Process tasks and collect yields
	for len(tasks) > 0 {
		for _, task := range tasks {
			if len(task.Yielded) > 0 {
				yield := task.Yielded[0].String()
				yields = append(yields, yield)

				// Check channel visibility at each yield point
				switch yield {
				case "select_started":
					// Both channels should be visible after select starts
					checkChannels([]string{"select_ch1", "select_ch2"})
				case "ready_for_send":
					// Send value through runtime to ch1
					err := runtime.Send("select_ch1", lua.LString("value1"))
					assert.NoError(t, err)

				case "done":
					checkChannels([]string{})
				}
			}
		}
		tasks, err = runtime.Step(vm, tasks...)
		assert.NoError(t, err)
	}

	expectedYields := []string{
		"select_started",
		"ready_for_send",
		"select_completed",
		"done",
		"completed:value1",
	}
	assert.Equal(t, expectedYields, yields, "yields occurred in unexpected order")
}

func TestNamedChannelSelectDefaultCase(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	vm.SetContext(context.Background())

	err = vm.StartString(`
		-- Create named channels
		local ch1 = channel.named("default_ch1", 0)
		local ch2 = channel.named("default_ch2", 0)

		-- Select with default case
		local result = channel.select{
			ch1:case_receive(),
			ch2:case_receive(),
			default = true
		}

		assert(result.default == true, "should hit default case")
		coroutine.yield("select_with_default_complete")
	`, "test")
	assert.NoError(t, err)

	runtime := NewChannelRunner()
	tasks, err := runtime.Step(vm)
	assert.NoError(t, err)

	// Check that no channels are visible since select with default doesn't block
	channels := runtime.GetOpenChannels()
	assert.Equal(t, 0, len(channels), "expected no visible channels with default case")

	var yields []string
	for len(tasks) > 0 {
		for _, task := range tasks {
			if len(task.Yielded) > 0 {
				yields = append(yields, task.Yielded[0].String())
			}
		}
		tasks, err = runtime.Step(vm, tasks...)
		assert.NoError(t, err)
	}

	expectedYields := []string{"select_with_default_complete"}
	assert.Equal(t, expectedYields, yields)
}

func TestNamedChannelMultipleReceivers(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	vm.SetContext(context.Background())

	err = vm.StartString(`
		-- Create channels
		local ch = channel.named("test_channel", 0)
		local results = channel.new(3) -- To collect results in order
		local order = 1 -- Track order of reception
		
		-- Start 3 coroutines that will wait for values
		for i = 1, 3 do
			coroutine.spawn(function()
				local current = order
				order = order + 1
				local val = ch:receive()
				results:send({
					value = val,
					receiver = i,
					order = current -- Record order of setup
				})
				coroutine.yield("receiver_" .. i .. "_got_" .. tostring(val))
			end)
		end

		-- Notify test that receivers are ready
		coroutine.yield("receivers_ready")
		
		-- Collect results in order they were received
		local received = {}
		for i = 1, 3 do
			local result = results:receive()
			table.insert(received, result)
			coroutine.yield("collected_result_" .. i)
		end
		
		-- Verify all values were received
		assert(#received == 3, "should receive exactly 3 values")
		
		-- Sort by order of setup to ensure deterministic verification
		table.sort(received, function(a, b) return a.order < b.order end)
		
		-- First routine gets first value, second gets second, etc.
		assert(received[1].value == "value1", string.format(
			"wrong first value: got %s, receiver %d, order %d", 
			tostring(received[1].value), received[1].receiver, received[1].order))
		assert(received[2].value == "value2", string.format(
			"wrong second value: got %s, receiver %d, order %d",
			tostring(received[2].value), received[2].receiver, received[2].order))
		assert(received[3].value == "value3", string.format(
			"wrong third value: got %s, receiver %d, order %d",
			tostring(received[3].value), received[3].receiver, received[3].order))
		
		coroutine.yield("verification_complete")
	`, "test")
	assert.NoError(t, err)

	runtime := NewChannelRunner()
	tasks, err := runtime.Step(vm)
	assert.NoError(t, err)

	var yields []string
	var receiverCount int
	var valuesDelivered bool

	for len(tasks) > 0 {
		for _, task := range tasks {
			if len(task.Yielded) > 0 {
				yield := task.Yielded[0].String()
				yields = append(yields, yield)

				// Once we see receivers are ready, check channels and send values
				if yield == "receivers_ready" && !valuesDelivered {
					channels := runtime.GetOpenChannels()
					assert.Equal(t, 1, len(channels), "expected exactly one visible channel")
					assert.Equal(t, "test_channel", channels[0].Name, "unexpected channel name")
					assert.Equal(t, 3, channels[0].Refs, "expected 3 references to channel")

					// Send all values in one batch - order matters!
					err = runtime.Send("test_channel",
						lua.LString("value1"), // Should go to first waiting routine
						lua.LString("value2"), // Should go to second waiting routine
						lua.LString("value3"), // Should go to third waiting routine
					)
					assert.NoError(t, err)
					valuesDelivered = true
				}

				if strings.HasPrefix(yield, "receiver_") && strings.Contains(yield, "_got_") {
					receiverCount++
				}
			}
		}

		tasks, err = runtime.Step(vm, tasks...)
		assert.NoError(t, err)
	}

	// Verify key events occurred in order
	assert.Contains(t, yields, "receivers_ready", "missing receivers_ready signal")

	// Verify order of value reception
	var receiveOrder []string
	for _, yield := range yields {
		if strings.HasPrefix(yield, "receiver_") && strings.Contains(yield, "_got_") {
			receiveOrder = append(receiveOrder, yield)
		}
	}

	// Should be exactly 3 receive events
	assert.Equal(t, 3, len(receiveOrder), "wrong number of receive events")

	// Values should be received in order: value1, value2, value3
	assert.True(t, strings.Contains(receiveOrder[0], "_got_value1"),
		"first value should be value1, got: %s", receiveOrder[0])
	assert.True(t, strings.Contains(receiveOrder[1], "_got_value2"),
		"second value should be value2, got: %s", receiveOrder[1])
	assert.True(t, strings.Contains(receiveOrder[2], "_got_value3"),
		"third value should be value3, got: %s", receiveOrder[2])

	// Verify final verification completed
	assert.Contains(t, yields, "verification_complete", "missing final verification")

	// no pending named
	channels := runtime.GetOpenChannels()
	assert.Equal(t, 0, len(channels), "expected no visible channels after completion")

	// Count result collections
	collectionCount := 0
	for _, yield := range yields {
		if strings.HasPrefix(yield, "collected_result_") {
			collectionCount++
		}
	}
	assert.Equal(t, 3, collectionCount, "wrong number of results collected")
}

func TestBufferedNamedChannelWriteCapacity(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	vm.SetContext(context.Background())

	err = vm.StartString(`
        -- Create channels
        local ch = channel.named("buffered_channel", 3)
        local ready = channel.new(0)
        local done = channel.new(0)
        
        -- Start receiver coroutine
        coroutine.spawn(function()
            -- Signal we're starting
            ready:send("ready")
            
            -- Read all buffered values
            for i = 1, 4 do
                local val = ch:receive()
                coroutine.yield("read_" .. tostring(val))
            end
            
            -- Signal completion
            done:send("done")
            coroutine.yield("reader_complete")
        end)
        
        -- Wait for reader to be ready
        ready:receive()
        coroutine.yield("main_ready")
        
        -- Wait for completion
        local result = done:receive()
        coroutine.yield("all_complete")
    `, "test")
	assert.NoError(t, err)

	runtime := NewChannelRunner()
	tasks, err := runtime.Step(vm)
	assert.NoError(t, err)

	var yields []string
	var writesDone bool

	for len(tasks) > 0 {
		for _, task := range tasks {
			if len(task.Yielded) > 0 {
				yield := task.Yielded[0].String()
				yields = append(yields, yield)

				if yield == "main_ready" && !writesDone {
					// Check channel state
					channels := runtime.GetOpenChannels()
					assert.Equal(t, 1, len(channels), "channel should be visible with reader")
					assert.Equal(t, "buffered_channel", channels[0].Name)
					assert.Equal(t, 4, channels[0].Slots, "should have 3 buffer slots + 1 reader")

					// First try to send too many values at once
					err = runtime.Send("buffered_channel",
						lua.LString("value1"),
						lua.LString("value2"),
						lua.LString("value3"),
						lua.LString("value4"),
						lua.LString("value5"), // This should make it fail
					)
					assert.Error(t, err, "should fail when sending too many values")

					// Now send just enough values
					err = runtime.Send("buffered_channel",
						lua.LString("value1"),
						lua.LString("value2"),
						lua.LString("value3"),
						lua.LString("value4"),
					)
					assert.NoError(t, err, "should succeed when sending correct number of values")

					writesDone = true
				}
			}
		}

		tasks, err = runtime.Step(vm, tasks...)
		assert.NoError(t, err)
	}

	// Verify we saw all expected yields
	assert.Contains(t, yields, "main_ready", "main routine should become ready")
	assert.Contains(t, yields, "all_complete", "main routine should complete")

	// Check each value was read
	valuesRead := 0
	for _, yield := range yields {
		if strings.HasPrefix(yield, "read_value") {
			valuesRead++
		}
	}
	assert.Equal(t, 4, valuesRead, "should read all 4 values")

	// Verify final state
	channels := runtime.GetOpenChannels()
	assert.Equal(t, 0, len(channels), "no channels should remain visible")
}
