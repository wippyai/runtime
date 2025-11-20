# Lua Eval Module Specification

## Overview

The `eval` module provides functionality to compile and execute Lua code dynamically at runtime with isolation and security controls.

## Module Interface

### Module Loading

```lua
local eval = require("eval")
```

### Functions

#### eval.compile(source: string, method: string, options?: table)

Compiles Lua code into a reusable program that can be executed multiple times.

Parameters:
- `source`: Lua source code as a string
- `method`: Name of the function to export from the code
- `options`: Optional configuration table with:
  - `modules`: Array of module names to make available (e.g., {"json", "http"})
  - `imports`: Table mapping variable names to values to import into the environment
  - `timeout`: Execution timeout as duration string (e.g., "5s", "100ms")

Returns:
- `program`: Compiled program object (or nil on error)
- `error`: Compilation error message (or nil on success)

Security:
- Requires `eval.compile` permission

#### eval.run(config: table)

Compiles and immediately executes Lua code in a single operation.

Parameters:
- `config`: Configuration table with fields:
  - `source`: Lua source code (required)
  - `method`: Function name to call (optional, defaults to "main")
  - `args`: Array of arguments to pass to the function (optional)
  - `modules`: Array of module names to make available (optional)
  - `imports`: Table mapping variable names to import values (optional)
  - `timeout`: Execution timeout duration string (optional)

Returns:
- `result`: Return value from the executed function (or nil on error)
- `error`: Execution error message (or nil on success)

Security:
- Requires `eval.run` permission

### Program Methods

#### program:run(method: string, ...args)

Executes a compiled program by calling a specific function.

Parameters:
- `method`: Name of the function to call
- `...args`: Arguments to pass to the function

Returns:
- `result`: Return value from the executed function (or nil on error)
- `error`: Execution error message (or nil on success)

#### program:set_timeout(duration: string)

Sets the execution timeout for the program.

Parameters:
- `duration`: Timeout duration string (e.g., "5s", "100ms", "1m")

Returns:
- Nothing on success
- `false`, `error`: On invalid duration format

## Example Usage

```lua
local eval = require("eval")

-- Compile a program with specific method export
local code = [[
  function add(a, b)
    return a + b
  end

  function multiply(a, b)
    return a * b
  end
]]

-- Compile with "add" as the exported method
local program, err = eval.compile(code, "add", {
  timeout = "5s",
  modules = {"json"}  -- Make json module available
})
if err then
  print("Compilation error:", err)
  return
end

-- Execute the compiled program
local result, err = program:run("add", 5, 3)
if err then
  print("Execution error:", err)
else
  print("5 + 3 =", result)  -- Output: 8
end

-- Change timeout dynamically
program:set_timeout("10s")

-- One-shot execution using eval.run with config table
result, err = eval.run({
  source = [[
    function main(name)
      return "Hello, " .. name
    end
  ]],
  method = "main",  -- Optional, defaults to "main"
  args = {"World"}
})

if not err then
  print(result)  -- Output: Hello, World
end

-- Example with modules and imports
result, err = eval.run({
  source = [[
    function process_data(input)
      local result = json.encode({processed = input, multiplier = base})
      return result
    end
  ]],
  method = "process_data",
  args = {42},
  modules = {"json"},  -- Available modules
  imports = {base = 10}  -- Imported variables
})
```

## Security

The eval module enforces security permissions:
- `eval.compile`: Required to compile code
- `eval.run`: Required to execute code directly

These permissions must be granted through the security system before eval operations can be performed.

## Notes

- Compiled programs are isolated from the calling environment unless explicitly configured
- Timeouts protect against infinite loops or long-running code
- Programs can be reused multiple times after compilation
- Each program execution runs in its own context
