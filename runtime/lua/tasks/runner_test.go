package tasks

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/async"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	timemod "github.com/ponyruntime/pony/runtime/lua/modules/time"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"testing"
	"time"
)

func TestTasker_BasicExecution(t *testing.T) {
	logger := zap.NewNop()

	t.Run("simple task execution", func(t *testing.T) {
		// Create base VM with tasks module
		vm, err := engine.NewCVM(logger,
			engine.WithPreloaded("tasks", NewTaskModule().Loader),
			engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		)
		assert.NoError(t, err)
		defer vm.Close()

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

		// Create tasker with both layers
		tasker := NewTasker(
			logger,
			vm,
			channel.NewChannelLayer(),
			4096,
		)

		// Start the tasker
		statusCh, err := tasker.Start(context.Background(), "test_handler")
		assert.NoError(t, err)

		// Execute a task
		resultCh, err := tasker.Execute(context.Background(), "test", []lua.LValue{lua.LString("hello")})
		assert.NoError(t, err)

		// Verify task result
		select {
		case result := <-resultCh:
			assert.NoError(t, result.Error)
			assert.Equal(t, 1, len(result.Result))
			assert.Equal(t, "world", result.Result[0].String())
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for task result")
		}

		ctx, _ := context.WithTimeout(context.Background(), time.Second)

		// stop the tasker
		err = tasker.Stop(ctx)
		assert.NoError(t, err)

		// Verify final status
		select {
		case status := <-statusCh:
			assert.Contains(t, status, "exit")
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for engine exit")
		}
	})
}

func TestTasker_ErrorHandling(t *testing.T) {
	logger := zap.NewNop()

	t.Run("task failure handling", func(t *testing.T) {
		// Create VM with modules
		vm, err := engine.NewCVM(logger,
			engine.WithPreloaded("tasks", NewTaskModule().Loader),
			engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
			function error_handler()
				local inbox = tasks.channel()
				
				while true do
					local task, ok = inbox:receive()
					if not ok then
						break
					end
					
					if task:input() == "fail" then
						task:fail("expected failure")
					else
						task:complete("success")
					end
				end
				
				return "exit"
			end
		`
		err = vm.Import(script, "test", "error_handler")
		require.NoError(t, err)

		tasker := NewTasker(logger, vm, channel.NewChannelLayer(), 10)
		statusCh, err := tasker.Start(context.Background(), "error_handler")
		require.NoError(t, err)

		// Test successful task
		resultCh, err := tasker.Execute(context.Background(), "success_task", []lua.LValue{lua.LString("success")})
		require.NoError(t, err)

		result := <-resultCh
		assert.NoError(t, result.Error)
		assert.Equal(t, "success", result.Result[0].String())

		// Test failing task
		resultCh, err = tasker.Execute(context.Background(), "fail_task", []lua.LValue{lua.LString("fail")})
		require.NoError(t, err)

		result = <-resultCh
		assert.Error(t, result.Error)
		assert.Equal(t, "expected failure", result.Error.Error())

		// Proper shutdown
		require.NoError(t, tasker.Stop(context.Background()))
		assert.Contains(t, <-statusCh, "exit")
	})
}

func TestTasker_ConcurrentExecution(t *testing.T) {
	logger := zap.NewNop()

	t.Run("multiple concurrent tasks", func(t *testing.T) {
		vm, err := engine.NewCVM(logger,
			engine.WithPreloaded("tasks", NewTaskModule().Loader),
			engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
			function concurrent_handler()
				local inbox = tasks.channel(5) -- Buffer multiple tasks
				local count = 0
				
				while true do
					local task, ok = inbox:receive()
					if not ok then
						break
					end
					count = count + 1
					task:complete("task" .. count)
				end
				
				return count
			end
		`
		err = vm.Import(script, "test", "concurrent_handler")
		require.NoError(t, err)

		tasker := NewTasker(logger, vm, channel.NewChannelLayer(), 10)
		statusCh, err := tasker.Start(context.Background(), "concurrent_handler")
		require.NoError(t, err)

		// Launch multiple concurrent tasks
		resultChannels := make([]<-chan engine.Result, 5)
		for i := 0; i < 5; i++ {
			ch, err := tasker.Execute(context.Background(), "task", []lua.LValue{lua.LString("test")})
			require.NoError(t, err)
			resultChannels[i] = ch
		}

		// Collect results
		results := make(map[string]bool)
		for _, ch := range resultChannels {
			result := <-ch
			assert.NoError(t, result.Error)
			results[result.Result[0].String()] = true
		}

		// Verify we got unique results for all tasks
		assert.Equal(t, 5, len(results))
		for i := 1; i <= 5; i++ {
			assert.True(t, results[fmt.Sprintf("task%d", i)])
		}

		require.NoError(t, tasker.Stop(context.Background()))
		status := <-statusCh
		assert.Equal(t, status, lua.LNumber(5))
	})
}

func TestConsecutiveTasks(t *testing.T) {
	logger := zap.NewNop()

	// Create base VM with tasks module and channels
	vm, err := engine.NewCVM(logger,
		engine.WithPreloaded("tasks", NewTaskModule().Loader),
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	// Create tasker
	tasker := NewTasker(logger, vm, channel.NewChannelLayer(), 10)

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

	// Start tasker
	statusCh, err := tasker.Start(context.Background(), "test_handler")
	require.NoError(t, err)

	// Send three consecutive tasks
	outputs := make([]<-chan engine.Result, 3)
	for i := 0; i < 3; i++ {
		taskData := lua.LTable{}
		taskData.RawSetString("data", lua.LString(string([]byte{byte('A' + i)})))

		out, err := tasker.Execute(context.Background(), "task"+string([]byte{byte('1' + i)}), []lua.LValue{&taskData})
		assert.NoError(t, err)
		outputs[i] = out
	}

	// Collect results
	results := make([]string, 3)
	for i, out := range outputs {
		select {
		case result := <-out:
			assert.NoError(t, result.Error)
			assert.Equal(t, 1, len(result.Result))
			results[i] = result.Result[0].String()
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for task %d", i+1)
		}
	}

	// Verify results
	assert.Equal(t, "processed_A", results[0])
	assert.Equal(t, "processed_B", results[1])
	assert.Equal(t, "processed_C", results[2])

	// stop tasker
	err = tasker.Stop(context.Background())
	assert.NoError(t, err)

	// Verify final status
	select {
	case result := <-statusCh:
		resultTable := result.(*lua.LTable)
		assert.Equal(t, resultTable.RawGetInt(1), lua.LString("A"))
		assert.Equal(t, resultTable.RawGetInt(2), lua.LString("B"))
		assert.Equal(t, resultTable.RawGetInt(3), lua.LString("C"))
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for handler completion")
	}
}

func TestAsyncTasksWithTimers(t *testing.T) {
	logger := zap.NewNop()

	// Create base VM with required modules
	vm, err := engine.NewCVM(logger,
		engine.WithPreloaded("tasks", NewTaskModule().Loader),
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		engine.WithPreloaded("time", timemod.NewTimeModule().Loader),
	)
	require.NoError(t, err)
	defer vm.Close()

	// Create channel layer
	channels := channel.NewChannelLayer()

	// Create tasker with async support
	tasker := NewTasker(
		logger,
		vm,
		channels,
		10,
		engine.WithLayer(async.NewAsyncLayer(channels, 4096)),
	)

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

					-- wait for specified delay
					time.after(task:input().delay):receive()

					-- Record completion order
					table.insert(results, {
						id = task:input().id,
						original_order = task:input().order
					})

					-- Signal completion and send result back
					completed:send(true)
					task:complete("done_" .. task:input().id)
				end)
			end

			-- wait for all tasks to complete
			for i = 1, 3 do
				completed:receive()
			end

			return results
		end
	`
	err = vm.Import(script, "test", "test_handler")
	require.NoError(t, err)

	// Start tasker
	statusCh, err := tasker.Start(context.Background(), "test_handler")
	require.NoError(t, err)

	// Send tasks with different delays
	delays := []int{150, 50, 100} // Task A: 150ms, Task B: 50ms, Task C: 100ms
	outputs := make([]<-chan engine.Result, 3)

	for i := 0; i < 3; i++ {
		taskData := lua.LTable{}
		taskData.RawSetString("id", lua.LString(string([]byte{byte('A' + i)})))
		taskData.RawSetString("delay", lua.LNumber(delays[i]))
		taskData.RawSetString("order", lua.LNumber(i+1))

		out, err := tasker.Execute(context.Background(), "task"+string([]byte{byte('1' + i)}), []lua.LValue{&taskData})
		require.NoError(t, err)
		outputs[i] = out
	}

	// Collect results
	results := make([]string, 3)
	for i, out := range outputs {
		select {
		case result := <-out:
			assert.NoError(t, result.Error)
			assert.Equal(t, 1, len(result.Result))
			results[i] = result.Result[0].String()
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout waiting for task %d", i+1)
		}
	}

	// Verify task results
	assert.Equal(t, "done_A", results[0])
	assert.Equal(t, "done_B", results[1])
	assert.Equal(t, "done_C", results[2])

	// stop tasker
	err = tasker.Stop(context.Background())
	require.NoError(t, err)

	// Verify completion order
	select {
	case result := <-statusCh:
		table := result.(*lua.LTable)
		assert.Equal(t, "B", table.RawGetInt(1).(*lua.LTable).RawGetString("id").String())
		assert.Equal(t, "C", table.RawGetInt(2).(*lua.LTable).RawGetString("id").String())
		assert.Equal(t, "A", table.RawGetInt(3).(*lua.LTable).RawGetString("id").String())
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for completion")
	}
}

func TestTasker_TaskSend(t *testing.T) {
	logger := zap.NewNop()

	// Create VM with required modules
	vm, err := engine.NewCVM(logger,
		engine.WithPreloaded("tasks", NewTaskModule().Loader),
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
	)
	require.NoError(t, err)
	defer vm.Close()

	// Create tasker
	tasker := NewTasker(logger, vm, channel.NewChannelLayer(), 10)

	script := `
		function send_handler()
			local inbox = tasks.channel()
			
			while true do
				local task, ok = inbox:receive()
				if not ok then
					break
				end

				-- Send intermediate results
				task:send("progress", 50)
				task:send("progress", 75)
				
				-- Complete with final result
				task:complete("done", 100)
			end
			
			return "exit"
		end
	`
	err = vm.Import(script, "test", "send_handler")
	require.NoError(t, err)

	// Start tasker
	statusCh, err := tasker.Start(context.Background(), "send_handler")
	require.NoError(t, err)

	// Execute task
	resultCh, err := tasker.Execute(context.Background(), "test_task", []lua.LValue{lua.LString("test")})
	require.NoError(t, err)

	// Collect all results (both intermediate and final)
	var results [][]lua.LValue
	var isChannelOpen bool = true

	for isChannelOpen {
		select {
		case result, ok := <-resultCh:
			if !ok {
				isChannelOpen = false
				break
			}
			require.NoError(t, result.Error)
			results = append(results, result.Result)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for results")
		}
	}

	// Verify results
	require.Equal(t, 3, len(results), "should receive two progress updates and one completion")

	// Check first progress update
	assert.Equal(t, "progress", results[0][0].String())
	assert.Equal(t, float64(50), float64(results[0][1].(lua.LNumber)))

	// Check second progress update
	assert.Equal(t, "progress", results[1][0].String())
	assert.Equal(t, float64(75), float64(results[1][1].(lua.LNumber)))

	// Check final result
	assert.Equal(t, "done", results[2][0].String())
	assert.Equal(t, float64(100), float64(results[2][1].(lua.LNumber)))

	// stop tasker
	err = tasker.Stop(context.Background())
	require.NoError(t, err)

	// Verify final status
	select {
	case status := <-statusCh:
		assert.Contains(t, status, "exit")
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for exit")
	}
}

func setupVM(b *testing.B) (*TaskRunner, func()) {
	logger := zap.NewNop()

	vm, err := engine.NewCVM(logger,
		engine.WithPreloaded("tasks", NewTaskModule().Loader),
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
	)
	if err != nil {
		b.Fatal(err)
	}

	tasker := NewTasker(
		logger,
		vm,
		channel.NewChannelLayer(),
		1000,
	)

	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = tasker.Stop(ctx)
		vm.Close()
	}

	return tasker, cleanup
}

func BenchmarkSingleTaskExecution(b *testing.B) {
	tasker, cleanup := setupVM(b)
	defer cleanup()

	script := `
        function bench_handler()
            local inbox = tasks.channel()
            while true do
                local task, ok = inbox:receive()
                if not ok then break end
                task:complete(task:input())
            end
        end
    `

	err := tasker.cvm.Import(script, "bench", "bench_handler")
	if err != nil {
		b.Fatal(err)
	}

	statusCh, err := tasker.Start(context.Background(), "bench_handler")
	if err != nil {
		b.Fatal(err)
	}
	<-statusCh // wait for "engine started"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, err := tasker.Execute(context.Background(), "bench_task", []lua.LValue{lua.LString("test")})
		if err != nil {
			b.Fatal(err)
		}
		<-out // wait for completion
	}
}

func BenchmarkParallelTaskExecution(b *testing.B) {
	tasker, cleanup := setupVM(b)
	defer cleanup()

	script := `
        function bench_handler()
            local inbox = tasks.channel(100)
            while true do
                local task, ok = inbox:receive()
                if not ok then break end
                task:complete(task:input())
            end
        end
    `

	err := tasker.cvm.Import(script, "bench", "bench_handler")
	if err != nil {
		b.Fatal(err)
	}

	statusCh, err := tasker.Start(context.Background(), "bench_handler")
	if err != nil {
		b.Fatal(err)
	}
	<-statusCh // wait for "engine started"

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			out, err := tasker.Execute(context.Background(), "bench_task", []lua.LValue{lua.LString("test")})
			if err != nil {
				b.Fatal(err)
			}
			<-out // wait for completion
		}
	})
}

func BenchmarkTaskWithData(b *testing.B) {
	tasker, cleanup := setupVM(b)
	defer cleanup()

	script := `
        function bench_handler()
            local inbox = tasks.channel()
            while true do
                local task, ok = inbox:receive()
                if not ok then break end
                local data = task:input()
                task:complete(data)
            end
        end
    `

	err := tasker.cvm.Import(script, "bench", "bench_handler")
	if err != nil {
		b.Fatal(err)
	}

	statusCh, err := tasker.Start(context.Background(), "bench_handler")
	if err != nil {
		b.Fatal(err)
	}
	<-statusCh // wait for "engine started"

	// Create test data table
	testData := &lua.LTable{}
	testData.RawSetString("value", lua.LString("test"))
	testData.RawSetString("number", lua.LNumber(123))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, err := tasker.Execute(context.Background(), "bench_task", []lua.LValue{testData})
		if err != nil {
			b.Fatal(err)
		}
		<-out // wait for completion
	}
}
