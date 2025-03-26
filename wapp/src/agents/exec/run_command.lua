-- Execute a system command and return its output
-- @param params Table containing:
--   command (string): The command to execute
--   work_dir (string, optional): Working directory for command execution
--   env (table, optional): Environment variables as key-value pairs
--   timeout (string, optional): Maximum execution time (e.g., '30s', '1m')
-- @return Table containing:
--   stdout (string): Command's standard output
--   stderr (string): Command's standard error
--   exit_code (number): Process exit code
--   timed_out (boolean): Whether the command timed out
--   success (boolean): Whether the operation was successful
--   error (string, optional): Error message if operation failed
local exec = require("exec")
local time = require("time")
local json = require("json")

local function execute(params)
    -- Validate input
    if not params.command then
        return {
            success = false,
            error = "Missing required parameter: command"
        }
    end

    -- Get executor factory
    local executor = exec.get("app:native_executor")

    -- Create process options
    local options = {}
    if params.work_dir then
        options.work_dir = params.work_dir
    end
    if params.env then
        options.env = params.env
    end

    -- Create process with error handling
    local proc = executor:exec(params.command, options)

    proc:start()

    -- Track if the process timed out
    local timed_out = false

    -- Set up timeout if specified
    local timeout_timer
    if params.timeout then
        timeout_timer = time.timer(params.timeout, function()
            timed_out = true
            proc:close(true)
        end)
    end

    -- Capture stdout in a coroutine
    local stdout = ""
    coroutine.spawn(function()
        local stream = proc:stdout_stream()
        while true do
            local chunk = stream:read()
            if not chunk then break end
            stdout = stdout .. chunk
        end
        stream:close()
    end)

    -- Capture stderr in a coroutine
    local stderr = ""
    coroutine.spawn(function()
        local stream = proc:stderr_stream()
        while true do
            local chunk = stream:read()
            if not chunk then break end
            stderr = stderr .. chunk
        end
        stream:close()
    end)

    -- Wait for process to complete
    local exit_code = proc:wait()

    -- Cancel timeout timer if it exists
    if timeout_timer then
        timeout_timer:cancel()
    end

    -- Release resources
    executor:release()

    -- Return results
    return {
        success = true,
        stdout = stdout,
        stderr = stderr,
        exit_code = exit_code,
        timed_out = timed_out
    }
end

return execute
