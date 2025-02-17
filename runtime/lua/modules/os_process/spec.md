# Process Module Specification

## Buffer Size Limits

Default and limit values for buffer sizes:
* Read buffer: 64 kilobytes

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
    work_dir: string             # Working directory for the process
    env: Map<string,string>      # Environment variables, should be in the for of {KEY=VALUE} table
}

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
local envs = {PATH="/bin:/usr/bin",LANG="en_US.UTF-8"}
local cmd = process.new(name, {work_dir=string,env=envs}) -- full command here
```

### Process Control

```lua
-- Start the process
local success, err = cmd:start()
-- Send specific signal to the process
-- signal_type is integer signal number
cmd:signal(signal)
-- Wait for process completion
-- Blocking call, should be used in coroutine
cmd:wait()
-- Get current process state
local state = cmd:state()
-- Returns: "not_started" | "running" | "terminated"
```


## IO Operations
```lua
-- Write data to process stdin
-- data is string data
cmd:write(data)
-- Read all collected output (requires collect stdout/stderr = true)
local stdout = cmd:stdout_stream() -- returns a stderr stream
local stderr = cmd:stderr_stream() -- returns a stdout stream

-- Read stdout stream data
while true do
    local data = stdout:read()
    if not data then break end
    print(data)
end

-- Read stderr stream data
while true do
    local data = stderr:read()
    if not data then break end
    print(data)
end

```

## Complete Example
```lua
local process = require("process")
-- Create and configure process
local proc = process.new('cat /dev/urandom | hexdump -C')
-- Start process
local success, err = cmd:start() if not success then error("Failed to start process: " .. err) end
-- Write input data
cmd:write("Hello, World!")
-- Wait for process completion
cmd:wait()
-- Read output
local stdout = cmd:stdout_stream()
while true do
    local data = stdout:read()
    if not data then break end
    print(data)
end
-- Closing would be done automatically
```

## Notes
- There is no close() method to close the process. Closing would be done automatically when the process is terminated.