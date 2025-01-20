package tasks

import (
	"context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/async"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	timemod "github.com/ponyruntime/pony/runtime/lua/modules/time"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"strconv"
	"testing"
	"time"
)

func TestTasksSingleExecution(t *testing.T) {
	logger := zap.NewNop()

	t.Run("simple task execution", func(t *testing.T) {
		// Create base VM with tasks module
		vm, err := engine.NewCVM(logger,
			engine.WithPreloaded("tasks", NewModule().Loader),
			engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		)
		assert.NoError(t, err)

		// Create channel runner and task runner
		channelRunner := channel.NewChannelRunner()
		taskMixer := NewMixer(channelRunner, 10)

		// Create wrapped VM with both layers
		wrapped := engine.NewWrappedCVM(vm,
			engine.WithLayer(channelRunner),
			engine.WithLayer(taskMixer),
		)

		// Import test script that will handle scheduled tasks
		script := `
            function test_handler()
				local inbox = tasks.channel()	

				while true do
					local task, ok = inbox:receive()
					if not ok then
						break	
					end	
					assert(task:input() == "hello")
					task:complete("world")
				end

				return "exit"
            end
        `
		err = vm.Import(script, "test", "test_handler")
		assert.NoError(t, err)

		// Set up task group context
		ctx, cancel := context.WithCancel(engine.WithTaskGroup(context.Background(), wrapped.GetTaskGroup()))
		defer cancel()

		done := make(chan struct{}, 1)

		// Start execution
		go func() {
			result, err := wrapped.Execute(ctx, "test_handler")
			assert.NoError(t, err)
			if result != nil {
				assert.Equal(t, "exit", result.String())
			} else {
				t.Fatal("no result")
			}
			done <- struct{}{}
		}()

		// send task
		out, err := taskMixer.Send(ctx, "test", lua.LString("hello"))
		assert.NoError(t, err)

		select {
		case result := <-out:
			err = taskMixer.CloseOutbox(ctx)
			assert.NoError(t, err)
			assert.Equal(t, "world", result.Values[0].String())
			select {
			case <-done:
			case <-time.After(1 * time.Second):
				cancel()
				t.Fatal("timeout on close")
			}
		case <-time.After(1 * time.Second):
			t.Fatal("timeout")
		}
	})
}

func TestConsecutiveTasks(t *testing.T) {
	logger := zap.NewNop()

	// Create base VM with tasks module and channels
	vm, err := engine.NewCVM(logger,
		engine.WithPreloaded("tasks", NewModule().Loader),
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	// Create channel runner and task mixer
	channelRunner := channel.NewChannelRunner()
	taskMixer := NewMixer(channelRunner, 10)

	// Create wrapped VM with required layers
	wrapped := engine.NewWrappedCVM(vm,
		engine.WithLayer(channelRunner),
		engine.WithLayer(taskMixer),
	)

	// Import test script that will handle scheduled tasks
	script := `
		function test_handler()
			local inbox = tasks.channel()
			local count = 0
			local results = {}

			while true do
				local task, ok = inbox:receive()
				if not ok then
					break
				end

				count = count + 1
				results[count] = task:input().data
				task:complete("processed_" .. task:input().data)
			end

			return results
		end
	`
	err = vm.Import(script, "test", "test_handler")
	assert.NoError(t, err)

	// Set up task group context
	ctx, cancel := context.WithCancel(engine.WithTaskGroup(context.Background(), wrapped.GetTaskGroup()))
	defer cancel()

	// Start execution
	done := make(chan lua.LValue, 1)
	go func() {
		result, err := wrapped.Execute(ctx, "test_handler")
		assert.NoError(t, err)
		done <- result
	}()

	// Send three consecutive tasks
	outputs := make([]chan coroutine.Result, 3)
	for i := 0; i < 3; i++ {
		taskData := lua.LTable{}
		taskData.RawSetString("data", lua.LString(string([]byte{byte('A' + i)})))

		out, err := taskMixer.Send(ctx, "task"+strconv.Itoa(i+1), &taskData)
		assert.NoError(t, err)
		outputs[i] = out
	}

	// Collect results from each task
	results := make([]string, 3)
	for i, out := range outputs {
		select {
		case result := <-out:
			assert.NoError(t, result.Error)
			assert.Equal(t, 1, len(result.Values))
			results[i] = result.Values[0].String()
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for task %d", i+1)
		}
	}

	// Verify individual task results
	assert.Equal(t, "processed_A", results[0])
	assert.Equal(t, "processed_B", results[1])
	assert.Equal(t, "processed_C", results[2])

	// Close the task channel and verify final results
	err = taskMixer.CloseOutbox(ctx)
	assert.NoError(t, err)

	// Wait for handler to complete and verify results
	select {
	case result := <-done:
		resultTable := result.(*lua.LTable)
		assert.Equal(t, 3, resultTable.Len())
		assert.Equal(t, "A", resultTable.RawGetInt(1).String())
		assert.Equal(t, "B", resultTable.RawGetInt(2).String())
		assert.Equal(t, "C", resultTable.RawGetInt(3).String())
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for handler completion")
	}
}

func TestAsyncTasksWithTimers(t *testing.T) {
	logger := zap.NewNop()

	// Create base VM with all required modules
	vm, err := engine.NewCVM(logger,
		engine.WithPreloaded("tasks", NewModule().Loader),
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		engine.WithPreloaded("time", timemod.NewTimeModule().Loader),
	)
	assert.NoError(t, err)

	// Create runners and mixers
	channelRunner := channel.NewChannelRunner()
	asyncRunner := async.NewAsyncRunner(channelRunner)
	taskMixer := NewMixer(channelRunner, 10)

	// Create wrapped VM with all layers
	wrapped := engine.NewWrappedCVM(vm,
		engine.WithLayer(channelRunner),
		engine.WithLayer(asyncRunner),
		engine.WithLayer(taskMixer),
	)
	defer wrapped.Close()

	// Import test script that processes tasks with different delays
	script := `
		function test_handler()
			local inbox = tasks.channel(3)
			local results = {}
			local completed = channel.new(3)  -- For tracking completion

			-- Start three coroutines to handle tasks
			for i = 1, 3 do
				coroutine.spawn(function()
				
					local task, ok = inbox:receive()
					if not ok then return end

print("here")
					-- Wait for specified delay
					time.sleep("10ms"):receive()

					print("done_" .. task:input().id)

					-- Record completion order
					table.insert(results, {
						id = task:input().id,
						original_order = task.order
					})

					-- Signal completion and send result back
					completed:send(true)
					task:complete("done_" .. task:input().id)
				end)
			end

			-- Wait for all tasks to complete
			for i = 1, 3 do
				completed:receive()
			end

			return results
		end
	`
	err = vm.Import(script, "test", "test_handler")
	require.NoError(t, err)

	// Set up context with async channel
	ctx, cancel := context.WithCancel(engine.WithTaskGroup(context.Background(), wrapped.GetTaskGroup()))
	ctx = async.WithAsyncChannel(ctx)
	defer cancel()

	// Start execution
	done := make(chan lua.LValue, 1)
	go func() {
		result, err := wrapped.Execute(ctx, "test_handler")
		assert.NoError(t, err)
		done <- result
	}()

	// Send three tasks with different delays
	delays := []int{150, 50, 100} // Task A: 150ms, Task B: 50ms, Task C: 100ms
	outputs := make([]chan coroutine.Result, 3)

	for i := 0; i < 3; i++ {
		taskData := lua.LTable{}
		taskData.RawSetString("id", lua.LString(string([]byte{byte('A' + i)})))
		taskData.RawSetString("delay", lua.LNumber(delays[i]))
		taskData.RawSetString("order", lua.LNumber(i+1))

		out, err := taskMixer.Send(ctx, "task"+strconv.Itoa(i+1), &taskData)
		assert.NoError(t, err)
		outputs[i] = out
	}

	// Collect individual task results
	results := make([]string, 3)
	for i, out := range outputs {
		select {
		case result := <-out:
			assert.NoError(t, result.Error)
			assert.Equal(t, 1, len(result.Values))
			results[i] = result.Values[0].String()
		case <-time.After(2 * time.Second):
			cancel()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("timeout waiting for handler completion")
			}
			t.Fatalf("timeout waiting for task %d", i+1)
		}
	}

	// Verify individual task results were received
	assert.Equal(t, "done_A", results[0])
	assert.Equal(t, "done_B", results[1])
	assert.Equal(t, "done_C", results[2])

	// Close task channel
	err = taskMixer.CloseOutbox(ctx)
	assert.NoError(t, err)

	// Wait for handler to complete and verify execution order
	select {
	case result := <-done:
		resultTable := result.(*lua.LTable)
		assert.Equal(t, 3, resultTable.Len())

		// Tasks should complete in order of their delays: B (50ms), C (100ms), A (150ms)
		firstResult := resultTable.RawGetInt(1).(*lua.LTable)
		assert.Equal(t, "B", firstResult.RawGetString("id").String())
		assert.Equal(t, float64(2), float64(firstResult.RawGetString("original_order").(lua.LNumber)))

		secondResult := resultTable.RawGetInt(2).(*lua.LTable)
		assert.Equal(t, "C", secondResult.RawGetString("id").String())
		assert.Equal(t, float64(3), float64(secondResult.RawGetString("original_order").(lua.LNumber)))

		thirdResult := resultTable.RawGetInt(3).(*lua.LTable)
		assert.Equal(t, "A", thirdResult.RawGetString("id").String())
		assert.Equal(t, float64(1), float64(thirdResult.RawGetString("original_order").(lua.LNumber)))

	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for handler completion")
	}
}
