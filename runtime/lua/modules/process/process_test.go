package process

import (
	"context"
	"testing"

	apic "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/async"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	"github.com/ponyruntime/pony/runtime/lua/modules/stream"
	"github.com/ponyruntime/pony/runtime/lua/modules/time"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestProcessBasic(t *testing.T) {
	// Setup logger and context
	logger, _ := zap.NewDevelopment()
	ctx := context.Background()
	ctx = context.WithValue(ctx, apic.LoggerCtx, logger)

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
	chans := channel.NewChannelLayer()
	// Create a wrapped VM with async runner
	wrapped := engine.NewRunner(
		vm,
		engine.WithLayer(chans),
		engine.WithLayer(async.NewAsyncLayer(chans, 10)),
		engine.WithLayer(coroutine.NewCoroutineLayer()),
	)
	err = vm.Import(`
			function test_process_simple()
			    -- Create a new process
			    local proc = process.new('cat /dev/urandom | hexdump -C')
			    assert(proc ~= nil, "Process creation should succeed")
			    
			    -- Create done channel for synchronization
			    local done = channel.new()
			    
			    -- Create timer for timeout
			    local timeout = time.timer("2s")  -- 2 second timeout
			    
			    -- Start the process
			    proc:start()
			    
			    -- Spawn reader coroutine
			    coroutine.spawn(function()
			        -- Get stdout stream
			        local stream, err = proc:get_stdout()
			        if err then
			            error("Failed to create read stream: " .. err)
			        end
			        
			        -- Read from stream in a loop
			        while true do
			            -- Check for either new data or timeout
			            local result = channel.select{
			                timeout:channel():case_receive(),
			                default = true  -- Make the select non-blocking
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

	// Execute test and verify results
	result, err := wrapped.Execute(ctx, "test_process_simple")
	require.NoError(t, err)
	_ = result
}
