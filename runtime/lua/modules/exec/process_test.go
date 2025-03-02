package exec

import (
	"context"
	"github.com/ponyruntime/pony/api/logs"
	"testing"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"github.com/ponyruntime/pony/runtime/lua/modules/stream"
	"github.com/ponyruntime/pony/runtime/lua/modules/time"
	mocklogger "github.com/ponyruntime/pony/tests/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestProcessBasic(t *testing.T) {
	// Setup logger and context
	logger := zap.NewNop()
	ctx := context.Background()
	ctx = logs.WithLogger(ctx, logger)

	mod := NewModule()
	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("process", mod.Loader),
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		engine.WithPreloaded("stream", stream.NewStreamModule(logger).Loader),
		engine.WithPreloaded("time", time.NewTimeModule().Loader),
	)

	require.NoError(t, err)
	defer vm.Close()
	// Spawn a wrapped VM with async runner
	wrapped := engine.NewRunner(
		vm,
		engine.WithLayer(channel.NewChannelLayer()),
		engine.WithLayer(coroutine.NewCoroutineLayer()),
	)

	err = vm.Import(`
			function test_process_simple()
			    -- Spawn a new process
			    local proc = process.new('cat /dev/urandom | hexdump -C', {})
			    assert(proc ~= nil, "Process creation should succeed")
			    
			    -- Spawn done channel for synchronization
			    local done = channel.new()
			    
			    -- Spawn timer for timeout
			    local timeout = time.timer("2s")  -- 2 second timeout
			    
			    -- Launch the process
			    proc:start()
			    
			    -- Spawn reader coroutine
			    coroutine.spawn(function()
			        -- GetField stdout stream
			        local stream, err = proc:stdout_stream()
			        if err then
			            error("Failed to create read stream: " .. err)
			        end
			        
			        -- Read from stream in a loop
			        while true do
			            -- Check for either new data or timeout
			            local result = channel.select{
			                timeout:channel():case_receive(),
			                default = true  -- Spawn the select non-blocking
			            }
			            
			            if result.default then
			                -- No timeout yet, try to read
			                local chunk, err = stream:read()
			                if err then
			                    error("Error reading from stream: " .. err)
			                end
			                if not chunk then
			                    print("End of stream reached")
			                    break
			                end
			                print("Read chunk:", chunk)
			            else
			                -- Timeout occurred
			                print("Timeout occurred, halting read")
			                break
			            end
			        end
			        
			        -- Print total bytes read
			        print("Total bytes read:", stream:bytes_read())
			        
			        -- Close the stream
			        local err = stream:close()
			        if err then
			            error("Failed to close stream: " .. err)
			        end
			        
			        -- Signal completion
			        done:send(true)
			    end)
			    
			    -- Wait for completion
			    local _, ok = done:receive()
			    if ok then
			        print("Reading completed")
			    else
			        print("Done channel closed")
			    end
			    
			    -- Cleanup
			    timeout:stop()
			    done:close()
			end        `, "test", "test_process_simple")
	require.NoError(t, err)

	// Call test
	_, err = wrapped.Execute(ctx, "test_process_simple")
	require.NoError(t, err)
}

func TestWorkingDir(t *testing.T) {
	// Setup logger and context
	logger := zap.NewNop()
	ctx := context.Background()
	ctx = logs.WithLogger(ctx, logger)

	mod := NewModule()
	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("process", mod.Loader),
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		engine.WithPreloaded("stream", stream.NewStreamModule(logger).Loader),
		engine.WithPreloaded("time", time.NewTimeModule().Loader),
	)

	require.NoError(t, err)
	defer vm.Close()

	wrapped := engine.NewRunner(
		vm,
		engine.WithLayer(channel.NewChannelLayer()),
		engine.WithLayer(coroutine.NewCoroutineLayer()),
	)
	err = vm.Import(`
			function test_process_env_workdir()
			    -- Spawn a new process with env vars and working dir
			    local env = {
			        TEST_VAR = "hello_world",
			        PATH = "/usr/local/bin:/usr/bin:/bin"  -- Ensure basic PATH is set
			    }
			    
			    -- Spawn a process that echoes an env var and prints working dir
			    local proc = process.new('sh -c "echo $TEST_VAR && pwd"', {
			        work_dir = "/tmp",
			        env = env
			    })
			    assert(proc ~= nil, "Process creation should succeed")
			    
			    -- Spawn done channel for synchronization
			    local done = channel.new()
			    
			    -- Spawn timer for timeout
			    local timeout = time.timer("2s")
			    
			    -- Launch the process
			    proc:start()
			    
			    -- Spawn reader coroutine
			    coroutine.spawn(function()
			        -- GetField stdout stream
			        local stream = proc:stdout_stream()
			        assert(stream ~= nil, "Stream creation should succeed")
			        
			        local output = {}
			        
			        -- Read from stream in a loop
			        while true do
			            -- Check for either new data or timeout
			            local result = channel.select{
			                timeout:channel():case_receive(),
			                default = true  -- Spawn the select non-blocking
			            }
			            
			            if result.default then
			                -- No timeout yet, try to read
			                local chunk = stream:read()
			                if not chunk then
			                    break
			                end
			                table.insert(output, chunk)
			            else
			                -- Timeout occurred
			                error("Timeout occurred while reading output")
			            end
			        end
			        
			        -- Join all output chunks
			        local full_output = table.concat(output, "")
			        
			        -- Verify environment variable was correctly set
			        assert(string.match(full_output, "hello_world"), 
			            "Environment variable TEST_VAR not found in output")
			        
			        -- Verify working directory was correctly set
			        assert(string.match(full_output, "/tmp"), 
			            "Working directory not set to /tmp")
			        
			        -- Close the stream
			        stream:close()
			        
			        -- Signal completion
			        done:send(true)
			    end)
			    
			    -- Wait for completion
			    local _, ok = done:receive()
			    assert(ok, "Done channel should receive completion signal")
			    
			    -- Cleanup
			    timeout:stop()
			    done:close()
			    
			    print("Environment and working directory test completed successfully")
			end
`, "test", "test_process_env_workdir")
	require.NoError(t, err)

	_, err = wrapped.Execute(ctx, "test_process_env_workdir")
	require.NoError(t, err)
}

func TestWriteStdin(t *testing.T) {
	// Setup logger and context
	logger := zap.NewNop()
	ctx := context.Background()
	ctx = logs.WithLogger(ctx, logger)

	mod := NewModule()
	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("process", mod.Loader),
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		engine.WithPreloaded("stream", stream.NewStreamModule(logger).Loader),
		engine.WithPreloaded("time", time.NewTimeModule().Loader),
	)

	require.NoError(t, err)
	defer vm.Close()

	// Spawn a wrapped VM with async runner
	wrapped := engine.NewRunner(
		vm,
		engine.WithLayer(channel.NewChannelLayer()),
		engine.WithLayer(coroutine.NewCoroutineLayer()),
	)
	err = vm.Import(`
			function test_process_stdin()
			    -- Spawn a new process using 'cat' which will echo back whatever we write to stdin
			    local proc = process.new('cat', {})
			    assert(proc ~= nil, "Process creation should succeed")
			    
			    -- Launch the process
			    proc:start()
			    
			    -- Test data to write
			    local test_data = "Hello from stdin!\nThis is a test.\n"
			    
			    -- Write to stdin
			    proc:write_stdin(test_data)
			    
			    -- GetField stdout stream and read the output
			    local stream = proc:stdout_stream()
			    local output = stream:read()
			    
			    -- Verify the output matches what we wrote to stdin
			    assert(output == test_data, string.format("Output '%s' doesn't match input '%s'", output, test_data))
			    
			    -- Cleanup
			    stream:close()	
			end
`, "test", "test_process_stdin")
	require.NoError(t, err)

	_, err = wrapped.Execute(ctx, "test_process_stdin")
	require.NoError(t, err)
}

func TestMultiplyCallsToStream(t *testing.T) {
	// Setup logger and context
	l, oLogger := mocklogger.ZapTestLogger(zap.DebugLevel)
	ctx := context.Background()
	ctx = logs.WithLogger(ctx, l)

	mod := NewModule()
	vm, err := engine.NewCVM(
		l,
		engine.WithPreloaded("process", mod.Loader),
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		engine.WithPreloaded("stream", stream.NewStreamModule(l).Loader),
		engine.WithPreloaded("time", time.NewTimeModule().Loader),
	)

	require.NoError(t, err)
	defer vm.Close()

	// Spawn a wrapped VM with async runner
	wrapped := engine.NewRunner(
		vm,
		engine.WithLayer(channel.NewChannelLayer()),
		engine.WithLayer(coroutine.NewCoroutineLayer()),
	)
	err = vm.Import(`
			function test_process_env_workdir()
			    -- Spawn a new process with env vars and working dir
			    local env = {
			        TEST_VAR = "hello_world",
			        PATH = "/usr/local/bin:/usr/bin:/bin"  -- Ensure basic PATH is set
			    }
			    
			    -- Spawn a process that echoes an env var and prints working dir
			    local proc = process.new('sh -c "echo $TEST_VAR && pwd"', {
			        work_dir = "/tmp",
			        env = env
			    })
			    assert(proc ~= nil, "Process creation should succeed")
			    
			    -- Spawn done channel for synchronization
			    local done = channel.new()
			    
			    -- Spawn timer for timeout
			    local timeout = time.timer("2s")
			    
			    -- Launch the process
			    proc:start()
			    
			    -- Spawn reader coroutine
			    coroutine.spawn(function()
			        -- GetField stdout stream
			        local stream = proc:stdout_stream()
			        local stream = proc:stdout_stream()
			        local stream = proc:stdout_stream()
			        local stream = proc:stdout_stream()
			        local stream = proc:stdout_stream()
			        local stream = proc:stdout_stream()

			        assert(stream ~= nil, "Stream creation should succeed")
			        
			        local output = {}
			        
			        -- Read from stream in a loop
			        while true do
			            -- Check for either new data or timeout
			            local result = channel.select{
			                timeout:channel():case_receive(),
			                default = true  -- Spawn the select non-blocking
			            }
			            
			            if result.default then
			                -- No timeout yet, try to read
			                local chunk = stream:read()
			                if not chunk then
			                    break
			                end
			                table.insert(output, chunk)
			            else
			                -- Timeout occurred
			                error("Timeout occurred while reading output")
			            end
			        end
			        
			        -- Join all output chunks
			        local full_output = table.concat(output, "")
			        
			        -- Verify environment variable was correctly set
			        assert(string.match(full_output, "hello_world"), 
			            "Environment variable TEST_VAR not found in output")
			        
			        -- Verify working directory was correctly set
			        assert(string.match(full_output, "/tmp"), 
			            "Working directory not set to /tmp")
			        
			        -- Close the stream
			        stream:close()
			        
			        -- Signal completion
			        done:send(true)
			    end)
			    
			    -- Wait for completion
			    local _, ok = done:receive()
			    assert(ok, "Done channel should receive completion signal")
			    
			    -- Cleanup
			    timeout:stop()
			    done:close()
			    
			    print("Environment and working directory test completed successfully")
			end
`, "test", "test_process_env_workdir")
	require.NoError(t, err)

	_, err = wrapped.Execute(ctx, "test_process_env_workdir")
	require.NoError(t, err)

	// should be only 1 log entry confirming that the stream was created only once
	require.Equal(t, 1, oLogger.FilterMessageSnippet("[sync.Once]: creating a new stdout stream").Len())
}
