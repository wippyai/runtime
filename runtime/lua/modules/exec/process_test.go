package exec

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	apiexec "github.com/wippyai/runtime/api/service/exec"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/channel"
	"github.com/wippyai/runtime/runtime/lua/engine/coroutine"
	luatime "github.com/wippyai/runtime/runtime/lua/modules/time"
	"github.com/wippyai/runtime/service/exec/native"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func newTestContext() context.Context {
	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	return ctx
}

// --- Mock Resource Setup ---

// mockResource implements resource.Resource
type mockResource struct {
	id       registry.ID
	resValue any
	released bool
}

func (m *mockResource) Get() (any, error) {
	if m.released {
		return nil, resource.ErrResourceReleased
	}
	return m.resValue, nil
}

func (m *mockResource) Release() {
	m.released = true
}

// simpleExecutorProvider holds the actual executor factory and provides access to it.
type simpleExecutorProvider struct {
	factory apiexec.ProcessExecutor
}

// Acquire implements resource.Provider
func (p *simpleExecutorProvider) Acquire(_ context.Context, id registry.ID, _ resource.AccessMode) (resource.Resource[any], error) {
	// Simple mock provider: always return the factory wrapped in a mock resource handle.
	// Ignore mode and context for this test provider.
	return &mockResource{id: id, resValue: p.factory}, nil
}

// mockResourceRegistry implements resource.Registry
type mockResourceRegistry struct {
	providers map[registry.ID]resource.Provider // Store providers now
}

func newMockRegistry() *mockResourceRegistry {
	return &mockResourceRegistry{
		providers: make(map[registry.ID]resource.Provider),
	}
}

// Add now takes a Provider
func (m *mockResourceRegistry) Add(id registry.ID, provider resource.Provider) {
	m.providers[id] = provider
}

func (m *mockResourceRegistry) Acquire(ctx context.Context, id registry.ID, mode resource.AccessMode) (resource.Resource[any], error) {
	provider, ok := m.providers[id]
	if !ok {
		return nil, resource.ErrResourceNotFound
	}
	// Delegate acquisition to the actual provider
	return provider.Acquire(ctx, id, mode)
}

func (m *mockResourceRegistry) List() ([]registry.ID, error) {
	ids := make([]registry.ID, 0, len(m.providers))
	for id := range m.providers {
		ids = append(ids, id)
	}
	return ids, nil
}

func (m *mockResourceRegistry) Exists(id registry.ID) bool {
	_, ok := m.providers[id]
	return ok
}

//nolint:unused // ok for now
type typeError struct {
	expected string
	actual   any
}

//nolint:unused // ok for now
func (e typeError) Error() string {
	return "type error: expected " + e.expected + " got " + typeName(e.actual)
}

//nolint:unused // ok for now
func typeName(v any) string {
	if v == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%T", v)
}

// --- Test Setup Helper ---

func setupLuaWithExec(t *testing.T, logger *zap.Logger) (*engine.CoroutineVM, *engine.Runner, context.Context) {
	// 1. Create the actual native executor factory
	nativeFactory := native.NewNativeExecutor(logger, &apiexec.NativeExecutorConfig{})

	// 2. Create the simple provider that holds the factory
	execProvider := &simpleExecutorProvider{factory: nativeFactory}

	// 3. Create a mock resource registry
	mockRegistry := newMockRegistry()

	// 4. Add the *provider* to the registry
	testExecutorID := registry.ParseID("test:native_executor")
	mockRegistry.Add(testExecutorID, execProvider) // Add the provider, not the factory directly

	// Create the Lua exec module
	execMod := NewExecModule(logger) // Use the actual constructor

	// Create VM and Runner
	vm, err := engine.NewCVM(
		logger,
		// Preload necessary modules
		engine.WithLoader(execMod.Info().Name, execMod.Loader),
		engine.WithLoader("time", luatime.NewTimeModule().Loader),
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
	)
	require.NoError(t, err)

	runner := engine.NewRunner(
		vm,
		engine.WithLayer(channel.NewChannelLayer()),
		engine.WithLayer(coroutine.NewCoroutineLayer()),
	)

	// Create context with resource registry
	ctx := newTestContext()
	ctx = logs.WithLogger(ctx, logger)
	ctx = resource.WithRegistry(ctx, mockRegistry)

	// Cleanup function for the VM
	t.Cleanup(func() {
		vm.Close()
	})

	return vm, runner, ctx
}

// --- Tests ---

func TestProcessBasic(t *testing.T) {
	logger := zap.NewNop() // Use zaptest.NewLogger(t) for verbose logs
	vm, runner, ctx := setupLuaWithExec(t, logger)

	// Use raw string literal for Lua code
	err := vm.Import(
		`
        function test_process_simple()
            local exec = require("exec")
            local time = require("time")
            -- Use full names: coroutine, channel

            -- 1. Get the executor factory resource
            local executor = exec.get("test:native_executor")
            assert(executor ~= nil, "Executor factory should be acquired")

            -- 2. Create a new process using the factory (using 'exec')
            local proc = executor:exec('sh -c "echo hello && sleep 0.1 && echo world"', {})
            assert(proc ~= nil, "Process creation should succeed")

            local done = channel.new()
            local timeout = time.timer("2s") -- Assuming timer returns an object usable in select

            -- 3. Start the process
            proc:start()

            local output = ""
            coroutine.spawn(function()
                local stream = proc:stdout_stream()
                assert(stream ~= nil, "Stdout stream creation should succeed")
                while true do
                    local chunk, err_read = stream:read()
					--print(chunk)

                    if err_read then error("Stream read error: " .. err_read) end
                    if not chunk then print("EOF reached"); break end
                    print("Read:", chunk)
                    output = output .. chunk
                end
                local ok_close, err_close = stream:close()
                -- Lua stream:close might return true/false, check API
                if not ok_close then error("Stream close error: " .. tostring(err_close)) end
                -- Signal completion on the channel
                done:send(true)	
            end)

            -- Wait for completion or timeout using channel.select
            local result = channel.select{
                done:case_receive(),         
                timeout:channel():case_receive()     
            }

            -- Check which case was selected
            if result.channel == timeout then -- Compare with the timer object itself
                -- Timeout occurred first
                proc:close(true) -- Force close on timeout
                error("Test timed out")
            elseif result.channel == done then
                 -- Reading completed
                 if not result.ok then
                     error("Done channel closed unexpectedly")
                 end
                print("Reading completed successfully")
            else
                error("Unexpected channel selected")
            end

            done:close()

            -- 4. Wait for the process (optional, but good practice)
            local exit_code, err_wait = proc:wait()

            assert(exit_code == 0, "Process should exit with code 0, got: " .. tostring(exit_code))
            assert(err_wait == nil, "Wait should not return an error, got: " .. tostring(err_wait))

            -- 5. Close the process handle (idempotent if wait finished)
            local ok_close_proc = proc:close()
            assert(ok_close_proc, "Closing process handle should succeed")

             -- 6. Release the executor factory handle
            local ok_release_exec = executor:release()
            assert(ok_release_exec, "Releasing executor factory should succeed")

            assert(string.match(output, "hello"), "Output missing 'hello'")
            assert(string.match(output, "world"), "Output missing 'world'")
        end
        `, "test", "test_process_simple")
	require.NoError(t, err)

	_, err = runner.Execute(ctx, "test_process_simple")

	// get error stack
	if err != nil {
		fmt.Println(err)
	}

	require.NoError(t, err)
}

func TestWorkingDirAndEnv(t *testing.T) {
	// Skip on Windows as `pwd` and shell behavior differs
	if runtime.GOOS == "windows" {
		t.Skip("Skipping test on Windows due to shell differences")
	}

	logger := zap.NewNop()
	vm, runner, ctx := setupLuaWithExec(t, logger)
	tmpDir := t.TempDir() // Create a temporary directory

	// Use raw string literal
	err := vm.Import(
		`
		function test_process_env_workdir(work_dir_path_arg)
			local exec = require("exec")
			-- Use full names: channel, coroutine
		
			local executor = exec.get("test:native_executor")
			assert(executor, "Failed to get executor")
			local env = { TEST_VAR = "hello_world" }
		
			-- Use the argument
			local work_dir_path = work_dir_path_arg
		
			-- Command needs fixing - the printf newline is being interpreted literally
			-- Use separate echo commands instead
			local proc = executor:exec('sh -c "echo $TEST_VAR && pwd"', {
				work_dir = work_dir_path,
				env = env
			})
			assert(proc, "Failed to create process")
		
			local done = channel.new()
			proc:start()
			local full_output = ""
			coroutine.spawn(function()
				local stream = proc:stdout_stream()
				assert(stream, "Failed to get stdout stream")
				while true do
					local chunk, err = stream:read()
					if err then error("Read error: " .. err) end
					if not chunk then break end
					full_output = full_output .. chunk
				end
				stream:close()
				-- Signal completion
				done:send(true) 
			end)
		
			-- Wait for completion
			local value, ok = done:receive()
			assert(ok and value, "Failed to receive completion signal")
			done:close()
		
			local exit_code, err_wait = proc:wait()
			assert(exit_code == 0, "Exit code non-zero: " .. tostring(exit_code))
			assert(err_wait == nil, "Wait error: " .. tostring(err_wait))
			proc:close()
			executor:release()
			print("Full output:", full_output)
			
			-- Check if hello_world is in the output
			assert(string.find(full_output, "hello_world", 1, true), 
				   "Env var not found in output: " .. full_output)
			
			-- Check if the working directory is in the output
			assert(string.find(full_output, work_dir_path, 1, true), 
				   "Working directory not found in output: " .. full_output)
		
			print("Env/WD test completed")
		end
       `, "test", "test_process_env_workdir")
	require.NoError(t, err)

	// Execute by passing tmpDir as a Lua string argument
	_, err = runner.Execute(ctx, "test_process_env_workdir", lua.LString(tmpDir))
	require.NoError(t, err)
}

// func TestWriteStdinAndStderr(t *testing.T) {
//	logger := zap.NewNop()
//	vm, _, runner := setupLuaWithExec(t, logger)
//
//	// Use raw string literal
//	err := vm.Import(
//		`
//       function test_process_stdin_stderr()
//           local exec = require("exec")
//           -- Use full names: channel, coroutine
//
//           local executor = exec.get("test:native_executor")
//           assert(executor, "Failed to get executor")
//
//           -- Use 'exec' instead of 'new_process'
//           -- Use 'cat' to echo stdin to stdout, and redirect stdout to stderr
//           local proc = executor:exec('sh -c "cat >&2"', {})
//           assert(proc, "Failed to create process")
//
//           local done_read = channel.new()
//           proc:start()
//           local test_data = "Hello via stdin to stderr!\nLine 2."
//           local ok_write, err_write = proc:write_stdin(test_data)
//           assert(ok_write, "Write stdin should succeed")
//           assert(err_write == nil, "Write stdin should not error: " .. tostring(err_write))
//
//           -- Important: Close stdin to signal EOF to 'cat', otherwise it waits forever
//           -- proc:close_stdin() -- Assuming this method exists, if not, cat exiting on its own might be sufficient
//           -- Let's rely on reading stderr until cat exits (which it should after stdin pipe is closed implicitly by Go os/exec when write_stdin completes)
//
//           local stderr_output = ""
//           coroutine.spawn(function()
//               local stream = proc:stderr_stream() -- Read from stderr
//               assert(stream, "Failed to get stderr stream")
//               while true do
//                   local chunk, err = stream:read()
//                   if err then error("Stderr read error: " .. err) end
//                   if not chunk then break end -- EOF
//                   stderr_output = stderr_output .. chunk
//               end
//               stream:close()
//               -- Signal completion
//               done_read:send(true)
//           end)
//
//           -- Wait for stderr reading to complete
//           local value, ok = done_read:receive()
//           assert(ok and value, "Failed to receive stderr read completion signal")
//           done_read:close()
//
//           local exit_code, err_wait = proc:wait()
//            assert(exit_code == 0, "Exit code non-zero: " .. tostring(exit_code))
//            assert(err_wait == nil, "Wait error: " .. tostring(err_wait))
//           proc:close()
//           executor:release()
//           -- Verify the output read from stderr matches stdin input
//           assert(stderr_output == test_data, string.format("Stderr output '%s' doesn't match input '%s'", stderr_output, test_data))
//           print("Stdin/Stderr test completed successfully")
//       end
//       `, "test", "test_process_stdin_stderr")
//	require.NoError(t, err)
//
//	_, err = runner.Execute(vm.State().Context(), "test_process_stdin_stderr")
//	require.NoError(t, err)
//}

func TestProcessWaitAndClose(t *testing.T) {
	logger := zap.NewNop()
	vm, runner, ctx := setupLuaWithExec(t, logger)

	// Use raw string literal
	err := vm.Import(
		`
       function test_process_wait_close()
           local exec = require("exec")
           local time = require("time") -- Keep time module required
           -- No coroutine or channel usage here, so no aliases to remove for them

           local executor = exec.get("test:native_executor")
           assert(executor, "Failed to get executor")

           -- OK process
           local proc_ok = executor:exec('sh -c "sleep 0.1 && exit 0"', {})
           proc_ok:start()
           local exit_code_ok, err_wait_ok = proc_ok:wait()
           assert(exit_code_ok == 0, "OK exit code: " .. tostring(exit_code_ok))
           assert(err_wait_ok == nil, "OK wait error: " .. tostring(err_wait_ok))
           assert(proc_ok:close(), "Closing OK process")

           -- Error process
           local proc_err = executor:exec('sh -c "sleep 0.1 && exit 42"', {})
           proc_err:start()
           local exit_code_err, err_wait_err = proc_err:wait()
           assert(exit_code_err == 42, "Err exit code: " .. tostring(exit_code_err))
           assert(err_wait_err == nil, "Err wait error: " .. tostring(err_wait_err))
           assert(proc_err:close(), "Closing Err process")

           -- Close early (graceful)
           local proc_close = executor:exec('sh -c "sleep 5"', {})
           proc_close:start()
           time.sleep("50ms") -- Use time directly
           assert(proc_close:close(false), "Closing early")
           local exit_code_closed, err_wait_closed = proc_close:wait()
           print("Exit code after close:", exit_code_closed, "Error:", err_wait_closed)
           -- Assertion might be OS dependent, let's just check it returned.
           assert(exit_code_closed ~= nil or err_wait_closed ~= nil, "Wait after close should return values")

           -- Force close early
           local proc_force_close = executor:exec('sh -c "sleep 5"', {})
           proc_force_close:start()
           time.sleep("50ms") -- Use time directly
           assert(proc_force_close:close(true), "Force closing")
           local exit_code_fclosed, err_wait_fclosed = proc_force_close:wait()
           print("Exit code after force close:", exit_code_fclosed, "Error:", err_wait_fclosed)
           -- Assertion might be OS dependent (e.g. 137 for SIGKILL on Unix-like)
            assert(exit_code_fclosed ~= nil or err_wait_fclosed ~= nil, "Wait after force close should return values")

           executor:release()
           print("Wait/Close test completed")
       end
       `, "test", "test_process_wait_close")
	require.NoError(t, err)

	// Allow slightly longer for this test due to sleeps and waits
	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, err = runner.Execute(timeoutCtx, "test_process_wait_close")
	require.NoError(t, err)
}

// func TestMultiplyCallsToStream(t *testing.T) {
//	l, oLogger := mocklogger.ZapTestLogger(zap.DebugLevel)
//	vm, _, runner := setupLuaWithExec(t, l)
//
//	// Use raw string literal
//	err := vm.Import(
//		`
//       function test_multiple_stream_calls()
//           local exec = require("exec")
//           -- Use full names: channel, coroutine
//
//           local executor = exec.get("test:native_executor")
//           assert(executor, "Failed to get executor")
//
//           local proc = executor:exec('sh -c "echo hello"', {})
//           assert(proc, "Failed to create process")
//
//           local done = channel.new()
//           proc:start()
//           local full_output = ""
//           coroutine.spawn(function()
//               -- Call multiple times, should return same underlying stream wrapper
//               local stream1 = proc:stdout_stream()
//               local stream2 = proc:stdout_stream()
//               assert(stream1 ~= nil, "Stream1 failed")
//               assert(stream1 == stream2, "Should get same stream object")
//               while true do
//                   local chunk, err = stream1:read() -- Read using one of them
//                   if err then error("Read error: " .. err) end
//                   if not chunk then break end -- EOF
//                   full_output = full_output .. chunk
//               end
//               stream1:close() -- Close one of them
//               -- Signal completion
//               done:send(true)
//           end)
//
//           -- Wait for completion
//           local value, ok = done:receive()
//           assert(ok and value, "Failed to receive completion signal")
//           done:close()
//
//           local exit_code, err_wait = proc:wait()
//            assert(exit_code == 0, "Exit code non-zero: " .. tostring(exit_code))
//            assert(err_wait == nil, "Wait error: " .. tostring(err_wait))
//           proc:close()
//           executor:release()
//           assert(string.find(full_output, "hello"), "Output missing hello: " .. full_output)
//           print("Multiple stream calls test completed successfully")
//       end
//       `, "test", "test_multiple_stream_calls")
//	require.NoError(t, err)
//
//	_, err = runner.Execute(vm.State().Context(), "test_multiple_stream_calls")
//	require.NoError(t, err)
//
//	// Check log output for sync.Once message
//	// Log message comes from initializeStreams in process.go
//	require.Equal(t, 1, oLogger.FilterMessageSnippet("Initializing stdout/stderr streams via sync.Once").Len(),
//		"Stream initialization should only happen once")
//}
