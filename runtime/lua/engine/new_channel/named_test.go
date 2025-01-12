package channel

import (
	"context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"testing"
)

func TestNamedChannelVisibility(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCoroutineVM(
		context.Background(),
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.PushScript(`
		-- Create two named channels
		local ch1 = channel.named("channel1", 1)
		local ch2 = channel.named("channel2", 1)

		-- Only block on channel1
		local val = ch1:receive()

		coroutine.yield("blocked")
	`, "test")
	assert.NoError(t, err)

	runtime := NewRuntime()
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

	vm, err := engine.NewCoroutineVM(
		context.Background(),
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.PushScript(`
		-- Create named channels with different capacities
		local ch1 = channel.named("select_ch1", 0) -- unbuffered
		local ch2 = channel.named("select_ch2", 1) -- buffered
		local done = channel.new(0) -- regular channel for coordination

		-- Start select operation that will block on both named channels
		coroutine.go(function()
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

	runtime := NewRuntime()
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

	vm, err := engine.NewCoroutineVM(
		context.Background(),
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.PushScript(`
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

	runtime := NewRuntime()
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
