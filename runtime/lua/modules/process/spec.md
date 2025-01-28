# Process Module Specification

## Buffer Size Limits

Default and limit values for buffer sizes:
* Read buffer: 32 kilobytes
* Default buffer: 16 megabytes
* Minimum buffer: 1 megabyte
* Maximum buffer: 512 megabytes

## Core Types

### Process States

```
State: Enumeration Values:
- not_started
- running
- terminated
```

### Options

```
Options {
    work_dir: string      # Working directory for the process
    env: Map<string>      # Environment variables
    io_config: IOConfig   # IO configuration
    idle_timeout: number  # Timeout in seconds for process inactivity
}

IOConfig {
    collect_stdout: boolean  # Whether to collect stdout
    collect_stderr: boolean  # Whether to collect stderr
    max_buffer: number      # Maximum buffer size (defaults to DefaultMaxBuffer) }
```

## Result

```lua
Result { exit_code: number # Process exit code error: string # Error message if failed runtime: number # Process runtime in seconds }
```

## API Reference

### Process Creation

```lua
-- Create new process instance
local process = require("process")
local cmd = process.new(name) -- full command here
-- Configure process options
cmd:set_options({ work_dir = string, env = table, idle_timeout = number, io_config = { collect_stdout = boolean, collect_stderr = boolean, max_buffer = number } })
```

### Process Control

```lua
-- Start the process
local success, err = cmd:start()
-- Stop the process (sends interrupt signal)
cmd:stop()
-- Send specific signal to the process
cmd:signal(signal_type)
-- Wait for process completion
local result = cmd:wait()
-- Returns: Result object
-- Get current process state
local state = cmd:state()
-- Returns: "not_started" | "running" | "terminated"
```


## IO Operations
```lua
-- Write data to process stdin
cmd:write(data)
-- Read all collected output (requires collect stdout/stderr = true)
local output = cmd:stdout() -- returns all stdout collected so far
local errors = cmd:stderr() -- returns all stderr collected so far
-- Stream output with separate handlers
cmd:on_stdout(function(data)) -- todo: use iterator instead? -- handle stdout data chunk end)
cmd:on_stderr(function(data)) -- todo: use iterator instead? -- handle stderr data chunk end)
-- Or use combined stream (simpler but less control)
cmd:on_output(function(data, source)) -- todo: use iterator instead?
-- source is "stdout" or "stderr" -- data is the output chunk end)
```

## Complete Example
```lua
local process = require("process")
-- Create and configure process
local cmd = process.new("python", {"script.py"}) cmd:set_options({ work_dir = "/scripts", env = { PYTHONPATH = "/usr/lib/python" }, idle_timeout = 10, io_config = { collect_stdout = true, collect_stderr = true, max_buffer = 1024 * 1024 } }) --1MB buffer
-- Start process
local success, err = cmd:start() if not success then error("Failed to start process: " .. err) end
-- Write input data
cmd:write("input data\n")
-- Stream output
cmd:on_output(function(data, source) if source == "stdout" then print("OUT:", data) else print("ERR:", data) end end)
-- Wait for completion
local result = cmd:wait() if result.exit_code ~= 0 then error("Process failed: " .. result.error) end print("Process completed in " .. result.runtime .. " seconds")
```
