package channel

import (
	"context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"testing"
)

func TestSelectImmediate(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCoroutineVM(
		context.Background(),
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.PushScript(`
		-- Create two buffered channels
		local ch1 = channel.new(1)
		local ch2 = channel.new(1)

		-- Send a value to ch1
		ch1:send("msg1")
		coroutine.yield("value_buffered")

		-- Try select on both channels
		local result = channel.select{
			ch1:case_receive(),
			ch2:case_receive()
		}

		-- Should immediately select ch1 since it has a value
		assert(result.channel == ch1, "wrong channel selected")
		assert(result.value == "msg1", "wrong value received")
		assert(result.ok == true, "receive should succeed")
		coroutine.yield("select_complete")

	`, "test")
	assert.NoError(t, err)

	runtime := NewRuntime()
	tasks, err := runtime.Step(vm)
	assert.NoError(t, err)

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

	expectedOrder := []string{
		"value_buffered",
		"select_complete",
	}

	assert.Equal(t, expectedOrder, yields, "yields occurred in unexpected order")
}

func TestSelectBlockedReceive(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCoroutineVM(
		context.Background(),
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.PushScript(`
		-- Create two unbuffered channels
		local ch1 = channel.new(0)
		local ch2 = channel.new(0)
		
		-- Start select operation that will block
		coroutine.go(function()
			local result = channel.select{
				ch1:case_receive(),
				ch2:case_receive()
			}
			coroutine.yield("select_completed:" .. result.value)
		end)
		
		coroutine.yield("select_started")
		ch2:send("ch2_value")
		coroutine.yield("send_completed")
	`, "test")
	assert.NoError(t, err)

	runtime := NewRuntime()
	tasks, err := runtime.Step(vm)
	assert.NoError(t, err)

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

	expectedOrder := []string{
		"select_started",
		"select_completed:ch2_value",
		"send_completed",
	}

	assert.Equal(t, expectedOrder, yields, "yields occurred in unexpected order")
}

func TestSelectBlockedClose(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCoroutineVM(
		context.Background(),
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.PushScript(`
		-- Create two unbuffered channels
		local ch1 = channel.new(0)
		local ch2 = channel.new(0)
		
		-- Start select operation that will block
		coroutine.go(function()
			local result = channel.select{
				ch1:case_receive(),
				ch2:case_receive()
			}
			-- For closed channel, value should be nil and ok should be false
			assert(result.value == nil, "value should be nil for closed channel")
			assert(result.ok == false, "ok should be false for closed channel")
			coroutine.yield("select_completed")
		end)
		
		coroutine.yield("select_started")
		
		-- Close ch1, this should wake up select
		ch1:close()
		coroutine.yield("close_completed")
	`, "test")
	assert.NoError(t, err)

	runtime := NewRuntime()
	tasks, err := runtime.Step(vm)
	assert.NoError(t, err)

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

	expectedOrder := []string{
		"select_started",
		"select_completed",
		"close_completed",
	}

	assert.Equal(t, expectedOrder, yields, "yields occurred in unexpected order")
}

func TestSelectWithDefaultImmediate(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCoroutineVM(
		context.Background(),
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.PushScript(`
        -- Helper to get channel stats
        local function channel_stats(ch)
            return {
                size = ch:_debug_size(),
                senders = ch:_debug_senders(),
                receivers = ch:_debug_receivers()
            }
        end

        -- Create two empty channels
        local ch1 = channel.new(0)
        local ch2 = channel.new(0)

        -- Try select with default
        local result = channel.select{
            ch1:case_receive(),
            ch2:case_receive(),
            default = true
        }

        assert(result.default == true, "should select default")

        -- Verify no pending operations
        local stats1 = channel_stats(ch1)
        local stats2 = channel_stats(ch2)
        
        assert(stats1.size == 0, "ch1 should be empty")
        assert(stats1.senders == 0, "ch1 should have no senders")
        assert(stats1.receivers == 0, "ch1 should have no receivers")
        
        assert(stats2.size == 0, "ch2 should be empty")
        assert(stats2.senders == 0, "ch2 should have no senders")
        assert(stats2.receivers == 0, "ch2 should have no receivers")

        coroutine.yield("select_complete")
    `, "test")
	assert.NoError(t, err)

	runtime := NewRuntime()
	tasks, err := runtime.Step(vm)
	assert.NoError(t, err)

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

	expectedOrder := []string{"select_complete"}
	assert.Equal(t, expectedOrder, yields)
}

func TestSelectLoopWithFeeds(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCoroutineVM(
		context.Background(),
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.PushScript(`
        -- Helper for channel stats
        local function channel_stats(ch)
            return {
                size = ch:_debug_size(),
                senders = ch:_debug_senders(),
                receivers = ch:_debug_receivers()
            }
        end
        
        local ch1 = channel.new(0)
        local ch2 = channel.new(0)
        local done = channel.new(0)
        
        -- Start select loop in goroutine
        coroutine.go(function()
            local count = 0
            while count < 2 do
                local result = channel.select{
                    ch1:case_receive(),
                    ch2:case_receive()
                }
                count = count + 1
                coroutine.yield("received:" .. result.value)
            end
            done:send("done")
        end)
        
        coroutine.yield("loop_started")
        
        -- Feed values from main goroutine
        ch1:send("val1")
        coroutine.yield("sent1")
        
        ch2:send("val2")
        coroutine.yield("sent2")
        
        -- Wait for completion
        done:receive()
        coroutine.yield("complete")
        
        -- Verify cleanup
        local stats1 = channel_stats(ch1)
        local stats2 = channel_stats(ch2)
        assert(stats1.receivers == 0, "ch1 should have no receivers")
        assert(stats2.receivers == 0, "ch2 should have no receivers")
    `, "test")
	assert.NoError(t, err)

	runtime := NewRuntime()
	tasks, err := runtime.Step(vm)
	assert.NoError(t, err)

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

	expectedOrder := []string{
		"loop_started",
		"received:val1",
		"sent1",
		"received:val2",
		"sent2",
		"complete",
	}
	assert.Equal(t, expectedOrder, yields)
}

func TestSelectCleanupOnReceive(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCoroutineVM(
		context.Background(),
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.PushScript(`
       local function channel_stats(ch)
           return {
               size = ch:_debug_size(),
               senders = ch:_debug_senders(),
               receivers = ch:_debug_receivers()
           }
       end

       local ch1 = channel.new(0)
       local ch2 = channel.new(0)

       -- Start blocked select
       coroutine.go(function()
           local result = channel.select{
               ch1:case_receive(),
               ch2:case_receive()
           }
           coroutine.yield("selected:" .. result.value)

           -- Check cleanup after select completes
           local stats1 = channel_stats(ch1)
           local stats2 = channel_stats(ch2)
           assert(stats1.receivers == 0, "ch1 should have no receivers")
           assert(stats2.receivers == 0, "ch2 should have no receivers")
       end)

       coroutine.yield("select_started")

       -- Verify both channels have 1 receiver from select
       local stats1 = channel_stats(ch1)
       local stats2 = channel_stats(ch2)
       assert(stats1.receivers == 1, "ch1 should have 1 receiver")
       assert(stats2.receivers == 1, "ch2 should have 1 receiver")

       ch1:send("val1")
       coroutine.yield("send_complete")
   `, "test")
	assert.NoError(t, err)

	runtime := NewRuntime()
	tasks, err := runtime.Step(vm)
	assert.NoError(t, err)

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

	expectedOrder := []string{
		"select_started",
		"selected:val1",
		"send_complete",
	}
	assert.Equal(t, expectedOrder, yields)
}

func TestSelectCleanupAll(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCoroutineVM(
		context.Background(),
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.PushScript(`
		local function channel_stats(ch)
			return {
				size = ch:_debug_size(),
				senders = ch:_debug_senders(),
				receivers = ch:_debug_receivers()
			}
		end

		local function verify_no_ops(ch1, ch2)
			local stats1 = channel_stats(ch1)
			local stats2 = channel_stats(ch2)
			assert(stats1.receivers == 0, "ch1 should have no receivers")
			assert(stats1.senders == 0, "ch1 should have no senders")
			assert(stats2.receivers == 0, "ch2 should have no receivers")
			assert(stats2.senders == 0, "ch2 should have no senders")
		end

		-- Test 1: Immediate select cleanup
		local ch1 = channel.new(1) -- buffered
		local ch2 = channel.new(0) -- unbuffered
		
		ch1:send("val1") -- buffer a value
		coroutine.yield("value_buffered")

		-- Should immediately select ch1 since it has buffered value
		local result = channel.select{
			ch1:case_receive(),
			ch2:case_receive(),
		}
		assert(result.value == "val1", "should receive buffered value")
		
		-- Verify no leftover operations
		verify_no_ops(ch1, ch2)
		coroutine.yield("immediate_select_cleaned")

		-- Test 2: Blocking select cleanup
		local ch3 = channel.new(0)
		local ch4 = channel.new(0)

		coroutine.go(function()
			local result = channel.select{
				ch3:case_receive(),
				ch4:case_receive()
			}
			assert(result.value == "val2", "should receive sent value")
			verify_no_ops(ch3, ch4)
			coroutine.yield("blocking_select_cleaned")
		end)
		
		coroutine.yield("select_started")

		-- Verify both channels have pending select
		local stats3 = channel_stats(ch3)
		local stats4 = channel_stats(ch4)
		assert(stats3.receivers == 1, "ch3 should have 1 receiver")
		assert(stats4.receivers == 1, "ch4 should have 1 receiver")
		coroutine.yield("select_verified")

		-- Wake up select
		ch3:send("val2")
		coroutine.yield("select_completed")
	`, "test")
	assert.NoError(t, err)

	runtime := NewRuntime()
	tasks, err := runtime.Step(vm)
	assert.NoError(t, err)

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

	expectedOrder := []string{
		"value_buffered",
		"immediate_select_cleaned",
		"select_started",
		"select_verified",
		"blocking_select_cleaned",
		"select_completed",
	}
	assert.Equal(t, expectedOrder, yields, "yields occurred in unexpected order")
}

// TestMixedSelectImmediate tests select with both send and receive cases
// where one case can be immediately selected
func TestMixedSelectImmediate(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCoroutineVM(
		context.Background(),
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.PushScript(`
		local function channel_stats(ch)
			return {
				size = ch:_debug_size(),
				senders = ch:_debug_senders(),
				receivers = ch:_debug_receivers()
			}
		end

		-- Create channels with different states
		local readyCh = channel.new(1)    -- buffered, will have a value
		local emptyCh = channel.new(1)    -- buffered, empty
		local fullCh = channel.new(1)     -- buffered, full
		
		-- Setup channel states
		readyCh:send("ready_value")       -- value ready for receive
		fullCh:send("full")               -- no space for send in future
		
		coroutine.yield("channels_setup")
		
		-- Test 1: Select with ready receive and blocked send
		local result = channel.select{
			fullCh:case_send("blocked"),   -- would block			
			readyCh:case_receive(),        -- should be selected (immediate)	
		}
		
		assert(result.channel == readyCh, "wrong channel selected")
		assert(result.value == "ready_value", "wrong value received")
		assert(result.ok == true, "receive should succeed")
		coroutine.yield("test1_complete")
		
		-- Test 2: Select with ready send and blocked receive
		local result2 = channel.select{
			emptyCh:case_send("new_value"), -- should be selected (immediate)
			fullCh:case_receive()           -- immediate select, but second in order
		}
		
		assert(result2.channel == emptyCh, "wrong channel selected")
		assert(result2.ok == true, "send should succeed")
		coroutine.yield("test2_complete")
		
		-- Verify no lingering operations
		local emptyStats = channel_stats(emptyCh)
		local fullStats = channel_stats(fullCh)
		
		assert(emptyStats.senders == 0, "emptyStats should have no pending senders, got " .. emptyStats.senders)
		assert(emptyStats.receivers == 0, "emptyStats should have no pending receivers, got " .. emptyStats.receivers)
		assert(fullStats.senders == 1, "fullStats should have 1 pending sender, got " .. fullStats.senders)
		assert(fullStats.receivers == 0, "fullStats should have no pending receivers, got " .. fullStats.receivers)
	`, "test")
	assert.NoError(t, err)

	runtime := NewRuntime()
	tasks, err := runtime.Step(vm)
	assert.NoError(t, err)

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

	expectedOrder := []string{
		"channels_setup",
		"test1_complete",
		"test2_complete",
	}
	assert.Equal(t, expectedOrder, yields)
}

// TestMixedSelectBlocking tests select with both send and receive cases
// where all cases initially block and are then unblocked by other goroutines
func TestMixedSelectBlocking(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCoroutineVM(
		context.Background(),
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.PushScript(`
		-- Create unbuffered channels
		local ch1 = channel.new(0)
		local ch2 = channel.new(0)
		local resultCh = channel.new(1)  -- for test coordination
		
		-- Start a goroutine that will do mixed select
		coroutine.go(function()
			local result = channel.select{
				ch1:case_send("value1"),    -- might be selected
				ch2:case_receive()          -- might be selected
			}
			resultCh:send(result)
		end)
		
		coroutine.yield("select_started")
		
		-- Start helper goroutine to trigger one of the cases
		coroutine.go(function()
			ch2:send("value2")  -- trigger the receive case
			coroutine.yield("helper_complete")
		end)
		
		-- Wait for and verify select result
		local result = resultCh:receive()
		assert(result.channel == ch2, "wrong channel selected")
		assert(result.value == "value2", "wrong value received")
		assert(result.ok == true, "operation should succeed")
		
		coroutine.yield("test_complete")
		
		-- Verify cleanup
		local function channel_stats(ch)
			return {
				size = ch:_debug_size(),
				senders = ch:_debug_senders(),
				receivers = ch:_debug_receivers()
			}
		end
		
		local stats1 = channel_stats(ch1)
		local stats2 = channel_stats(ch2)
		
		assert(stats1.senders == 0, "ch1 should have no pending senders")
		assert(stats1.receivers == 0, "ch1 should have no pending receivers")
		assert(stats2.senders == 0, "ch2 should have no pending senders")
		assert(stats2.receivers == 0, "ch2 should have no pending receivers")
	`, "test")
	assert.NoError(t, err)

	runtime := NewRuntime()
	tasks, err := runtime.Step(vm)
	assert.NoError(t, err)

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

	expectedOrder := []string{
		"select_started",
		"helper_complete",
		"test_complete",
	}
	assert.Equal(t, expectedOrder, yields)
}

// TestMixedSelectWithDefault tests select with both send and receive cases
// and a default case
func TestMixedSelectWithDefault(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCoroutineVM(
		context.Background(),
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.PushScript(`
		-- Create channels that would block
		local sendCh = channel.new(0)   -- unbuffered
		local recvCh = channel.new(0)   -- unbuffered
		
		-- Test 1: Select with all blocking cases should hit default
		local result = channel.select{
			sendCh:case_send("value"),
			recvCh:case_receive(),
			default = true
		}
		
		assert(result.default == true, "should hit default case")
		coroutine.yield("default_case_hit")
		
		-- Test 2: Select with one ready case should not hit default
		local bufferedCh = channel.new(1)
		bufferedCh:send("ready")
		
		local result2 = channel.select{
			sendCh:case_send("value"),
			bufferedCh:case_receive(),
			default = true
		}
		
		assert(result2.default == nil, "should not hit default")
		assert(result2.channel == bufferedCh, "wrong channel selected")
		assert(result2.value == "ready", "wrong value received")
		coroutine.yield("ready_case_selected")
		
		-- Verify no lingering operations
		local function channel_stats(ch)
			return {
				size = ch:_debug_size(),
				senders = ch:_debug_senders(),
				receivers = ch:_debug_receivers()
			}
		end
		
		local sendStats = channel_stats(sendCh)
		local recvStats = channel_stats(recvCh)
		local bufferedStats = channel_stats(bufferedCh)
		
		assert(sendStats.senders == 0 and sendStats.receivers == 0, "sendCh should be clean")
		assert(recvStats.senders == 0 and recvStats.receivers == 0, "recvCh should be clean")
		assert(bufferedStats.senders == 0 and bufferedStats.receivers == 0, "bufferedCh should be clean")
	`, "test")
	assert.NoError(t, err)

	runtime := NewRuntime()
	tasks, err := runtime.Step(vm)
	assert.NoError(t, err)

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

	expectedOrder := []string{
		"default_case_hit",
		"ready_case_selected",
	}
	assert.Equal(t, expectedOrder, yields)
}
