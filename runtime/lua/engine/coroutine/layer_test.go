package coroutine

import (
	"context"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/channel"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func newTestContext() context.Context {
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	return ctx
}

func TestRunner_AsLayer(t *testing.T) {
	// Setup VM
	log := zap.NewNop()
	vm, err := engine.NewCVM(log,
		engine.WithGlobalFunction("async_sleep", func(l *lua.LState) int {
			// Validate and get duration upfront
			ms := l.CheckNumber(1)

			Wrap(l, func() *engine.Update {
				time.Sleep(time.Duration(ms) * time.Millisecond)
				return engine.NewUpdate(nil, []lua.LValue{lua.LString("slept"), ms}, nil)
			})
			return -1
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()

	// Spawn wrapped VM with runner layer
	wrapped := engine.NewRunner(vm, engine.WithLayer(NewCoroutineLayer()))

	// Imports test script
	err = vm.Import(`
			function test_coroutine()
				local result = async_sleep(100)	
				return "done"
			end
		`, "test", "test_coroutine")

	if err != nil {
		t.Fatal(err)
	}

	// execute through layer
	result, err := wrapped.Execute(newTestContext(), "test_coroutine")
	if err != nil {
		t.Fatal(err)
	}

	if result.String() != "done" {
		t.Errorf("unexpected result: got %v, want 'done'", result)
	}
}

func TestAsyncCoroutines(t *testing.T) {
	log, _ := zap.NewDevelopment()

	// Spawn base VM with sleep function
	vm, err := engine.NewCVM(log,
		engine.WithGlobalFunction("async_sleep", func(l *lua.LState) int {
			// Validate and get duration upfront
			ms := l.CheckNumber(1)

			Wrap(l, func() *engine.Update {
				time.Sleep(time.Duration(ms) * time.Millisecond)

				return engine.NewUpdate(nil, []lua.LValue{lua.LString("slept"), ms}, nil)
			})

			return -1
		}),
	)
	require.NoError(t, err)
	defer vm.Close()

	// Spawn a wrapped VM with async runner
	wrapped := engine.NewRunner(vm, engine.WithLayer(NewCoroutineLayer()))

	// Imports test script with two coroutines
	err = vm.Import(`
          function test_sleep()
              local results = {}

              -- Launch first coroutine (longer sleep)
              coroutine.spawn(function()
                  local res1, dur1 = async_sleep(75)
                  results.first = {res1, dur1}
              end)

              -- Launch second coroutine (shorter sleep)
              coroutine.spawn(function()
                  local res2, dur2 = async_sleep(25)
                  results.second = {res2, dur2}
              end)

			   local res3, dur3 = async_sleep(100)
    		   results.third = {res3, dur3}

              return results
          end
      `, "test", "test_sleep")
	require.NoError(t, err)

	start := time.Now()
	ctx, cancel := context.WithTimeout(newTestContext(), time.Second*1000)
	defer cancel()

	result, err := wrapped.Execute(ctx, "test_sleep")
	duration := time.Since(start)
	require.NoError(t, err)

	// Verify results
	resultTable := result.(*lua.LTable)

	// Check first coroutine results
	firstRes := resultTable.RawGetString("first").(*lua.LTable)
	if firstRes.RawGetInt(1).String() != "slept" {
		t.Error("unexpected result from first coroutine")
	}
	if firstRes.RawGetInt(2).(lua.LNumber) != 75 {
		t.Error("unexpected duration from first coroutine")
	}

	// Check second coroutine results
	secondRes := resultTable.RawGetString("second").(*lua.LTable)
	if secondRes.RawGetInt(1).String() != "slept" {
		t.Error("unexpected result from second coroutine")
	}
	if secondRes.RawGetInt(2).(lua.LNumber) != 25 {
		t.Error("unexpected duration from second coroutine")
	}

	// Check second coroutine results
	thirdRes := resultTable.RawGetString("third").(*lua.LTable)
	if thirdRes.RawGetInt(1).String() != "slept" {
		t.Error("unexpected result from third result")
	}
	if thirdRes.RawGetInt(2).(lua.LNumber) != 100 {
		t.Error("unexpected duration from third result")
	}

	// Verify execution time - should be closer to 100ms than 150ms
	// since coroutines run concurrently
	if duration >= 150*time.Millisecond {
		t.Errorf("coroutines appear to be running sequentially, took %v", duration)
	}
}

func createVM(t *testing.T) *engine.CoroutineVM {
	log := zap.NewNop()

	vm, err := engine.NewCVM(log,
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		engine.WithGlobalFunction("async_double", func(l *lua.LState) int {
			// Validate argument first
			value := l.CheckNumber(1)

			Wrap(l, func() *engine.Update {
				time.Sleep(100 * time.Millisecond)
				return engine.NewUpdate(nil, []lua.LValue{value * 2}, nil)
			})

			return -1
		}),
	)

	assert.NoError(t, err)
	return vm
}

func TestRunner_ChannelLayer(t *testing.T) {
	testScript := `
       function test_layers()
           -- Channel for communication
           local ch = channel.new(1)

           -- Spawn worker that does async operation
           coroutine.spawn(function()
               local doubled = async_double(5)
               ch:send(doubled)
           end)

           -- wait for result
           local result = ch:receive()
           return result
       end
   `

	t.Run("channel first, async second", func(t *testing.T) {
		vm := createVM(t)
		defer vm.Close()

		wrapped := engine.NewRunner(vm,
			engine.WithLayer(channel.NewChannelLayer()),
			engine.WithLayer(NewCoroutineLayer()),
		)

		err := vm.Import(testScript, "test", "test_layers")
		assert.NoError(t, err)

		result, err := wrapped.Execute(newTestContext(), "test_layers")
		assert.NoError(t, err)
		assert.NotNil(t, result)

		numValue, ok := result.(lua.LNumber)
		assert.True(t, ok, "expected number result")
		assert.Equal(t, float64(10), float64(numValue))
	})

	t.Run("async first, channel second", func(t *testing.T) {
		vm := createVM(t)
		defer vm.Close()

		wrapped := engine.NewRunner(vm,
			engine.WithLayer(NewCoroutineLayer()),
			engine.WithLayer(channel.NewChannelLayer()),
		)

		err := vm.Import(testScript, "test", "test_layers")
		assert.NoError(t, err)

		result, err := wrapped.Execute(newTestContext(), "test_layers")
		assert.NoError(t, err)
		assert.NotNil(t, result)

		numValue, ok := result.(lua.LNumber)
		assert.True(t, ok, "expected number result")
		assert.Equal(t, float64(10), float64(numValue))
	})
}

func TestDistributedWorkers(t *testing.T) {
	testScript := `
       function test_distributed_workers()
           local NUM_WORKERS = 5
           local NUM_TASKS = 10
           local results = {}

           -- Spawn result channel with buffer size matching number of tasks
           local result_ch = channel.new(NUM_TASKS)

           -- Distribute work across workers
           for i = 1, NUM_WORKERS do
               coroutine.spawn(function()
                   -- Each worker processes their portion of tasks
                   local worker_id = i
                   for task = worker_id, NUM_TASKS, NUM_WORKERS do
                       local result = async_double(task)
                       result_ch:send({worker = worker_id, task = task, value = result})
                   end
               end)
           end

           -- Collect all results
           for i = 1, NUM_TASKS do
               local result = result_ch:receive()
               results[i] = result
           end

           return results
       end
   `

	t.Run("distributed work across workers", func(t *testing.T) {
		// Spawn VM with necessary modules
		vm := createVM(t)
		defer vm.Close()

		// Setup wrapped VM with required layers
		wrapped := engine.NewRunner(vm,
			engine.WithLayer(channel.NewChannelLayer()),
			engine.WithLayer(NewCoroutineLayer()),
		)

		// Imports test script
		err := vm.Import(testScript, "test", "test_distributed_workers")
		assert.NoError(t, err)

		// execute and time the operation
		start := time.Now()
		result, err := wrapped.Execute(newTestContext(), "test_distributed_workers")
		duration := time.Since(start)
		assert.NoError(t, err)

		// Verify results
		resultTable := result.(*lua.LTable)

		// We should have 10 results
		count := 0
		resultTable.ForEach(func(_, value lua.LValue) {
			count++
			result := value.(*lua.LTable)

			// Each result should have worker, task, and value fields
			worker := result.RawGetString("worker").(lua.LNumber)
			task := result.RawGetString("task").(lua.LNumber)
			value = result.RawGetString("value").(lua.LNumber)

			// Verify the doubled value is correct
			assert.Equal(t, task*2, value)

			// Verify worker Alias is in valid range
			assert.GreaterOrEqual(t, int(worker), 1)
			assert.LessOrEqual(t, int(worker), 5)
		})
		assert.Equal(t, 10, count, "should have 10 results")

		// Since we're running 10 tasks that each take 100ms
		// but distributing across 5 workers, it should take
		// approximately 200ms (2 tasks per worker)
		// AddCleanup some buffer for scheduling overhead
		assert.Less(t, duration, 350*time.Millisecond,
			"tasks should complete in parallel, got %v", duration)
	})
}

func TestWorkerPool(t *testing.T) {
	testScript := `
       function test_worker_pool()
           local NUM_WORKERS = 20
           local NUM_TASKS = 20

           -- Spawn channels for tasks and results
           local task_ch = channel.new(NUM_TASKS)
           local result_ch = channel.new(NUM_TASKS)
           local done_ch = channel.new(NUM_WORKERS) -- Channel to track worker completion

           -- Spawn workers
           for i = 1, NUM_WORKERS do
               coroutine.spawn(function()
                   local worker_id = i

                   -- Process tasks until channel is closed
                   while true do
                       local task, ok = task_ch:receive()
                       if not ok then
                           break -- Channel closed, exit worker
                       end

                       -- Process task
                       local result = async_double(task)
                       result_ch:send({
                           worker = worker_id,
                           task = task,
                           value = result
                       })
                   end

                   -- Signal worker completion
                   done_ch:send(worker_id)
               end)
           end

           -- SendToOpen all tasks
           for i = 1, NUM_TASKS do
               task_ch:send(i)
           end

           -- close task channel to signal no more tasks
           task_ch:close()

           -- Collect and sum all results
           local total = 0
           local received = 0
           local results = {}

           while received < NUM_TASKS do
               local result = result_ch:receive()
               received = received + 1
               total = total + result.value
               results[received] = result
           end

           -- wait for all workers to complete
           for i = 1, NUM_WORKERS do
               done_ch:receive()
           end

           return {
               sum = total,
               results = results,
               tasks_completed = received
           }
       end
   `

	t.Run("worker pool with result aggregation", func(t *testing.T) {
		// Spawn VM with necessary modules
		vm := createVM(t)
		defer vm.Close()

		// Setup wrapped VM with required layers
		wrapped := engine.NewRunner(vm,
			engine.WithLayer(channel.NewChannelLayer()),
			engine.WithLayer(NewCoroutineLayer()),
		)

		// Imports test script
		err := vm.Import(testScript, "test", "test_worker_pool")
		assert.NoError(t, err)

		// execute and time the operation
		start := time.Now()
		result, err := wrapped.Execute(newTestContext(), "test_worker_pool")
		duration := time.Since(start)
		assert.NoError(t, err)

		// Verify results
		resultTable := result.(*lua.LTable)

		// Check total sum - each number 1-20 is doubled, so sum should be (20 * 21) = 420
		sum := resultTable.RawGetString("sum").(lua.LNumber)
		assert.Equal(t, float64(420), float64(sum), "sum should be 420 (sum of doubled numbers 1-20)")

		// Verify number of completed tasks
		tasksCompleted := resultTable.RawGetString("tasks_completed").(lua.LNumber)
		assert.Equal(t, float64(20), float64(tasksCompleted), "should have completed 20 tasks")

		// Check individual results
		results := resultTable.RawGetString("results").(*lua.LTable)
		seenTasks := make(map[float64]bool)
		results.ForEach(func(_, value lua.LValue) {
			result := value.(*lua.LTable)

			// Verify worker Alias is in valid range
			worker := result.RawGetString("worker").(lua.LNumber)
			assert.GreaterOrEqual(t, int(worker), 1)
			assert.LessOrEqual(t, int(worker), 20)

			// Verify task and value
			task := result.RawGetString("task").(lua.LNumber)
			value = result.RawGetString("value").(lua.LNumber)
			assert.Equal(t, task*2, value,
				"result should be double the task value")

			// Ensure each task is processed exactly once
			assert.False(t, seenTasks[float64(task)],
				"task %v was processed multiple times", task)
			seenTasks[float64(task)] = true
		})

		// Verify all tasks were processed
		assert.Equal(t, 20, len(seenTasks), "all tasks should be processed exactly once")

		// Since we have 20 workers for 20 tasks, and each task takes 100ms,
		// this should complete in roughly 100ms plus some overhead for scheduling
		assert.Less(t, duration, 200*time.Millisecond,
			"tasks should complete in parallel, got %v", duration)
	})
}
