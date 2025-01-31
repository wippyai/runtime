# Temporal Workflow Library Specification

## Overview

The Temporal workflow library provides a set of utilities for building and managing workflow-based applications. It
offers abstractions for activities, timers, and parallel/race execution patterns using an underlying channel-based
communication system.

## Core Components

### Default Configuration

The library provides default configurations for activities:

```lua
{
    task_queue = "default",
    schedule_to_close_timeout = "30s",
    retry_policy = {
        initial_interval = "1s",
        backoff_coefficient = 2.0,
        maximum_interval = "100s",
        maximum_attempts = 3
    }
}
```

### Command Handle

A wrapper around workflow commands that provides a consistent interface for handling responses and errors.

Methods:

- `await()`: Blocks until command completes and returns the result
- `response()`: Returns the command's response channel
- `error()`: Returns the command's error information

## Core Functions

### Activity Management

#### `activity(name, config)`

Creates an activity definition that can be called multiple times.

Parameters:

- `name`: String identifying the activity
- `config`: Optional configuration table that merges with default config

Returns:

- Function that creates new activity command instances

#### `init_activities(activities_def)`

Creates a workflow scope containing multiple activity definitions.

Parameters:

- `activities_def`: Table mapping activity names to their definitions

Returns:

- Table of callable activity functions

Example:

```lua
local activities = wf.init_activities({
    hello_world = {
        name = "hello_world.activity",
        config = {
            task_queue = "custom_queue",
            schedule_to_close_timeout = "10s"
        }
    }
})
```

### Timer Operations

#### `sleep(duration)`

Creates a timer command that pauses execution for specified duration.

Parameters:

- `duration`: String duration (e.g., "5s", "1m")

Returns:

- CommandHandle for the timer

### Parallel Execution Patterns

#### `race(handles)`

Waits for the first command to complete among multiple handles.

Parameters:

- `handles`: Array of CommandHandles

Returns:

- Result of the first completed command

#### `parallel(handles)`

Waits for all commands to complete.

Parameters:

- `handles`: Array of CommandHandles

Returns:

- Array of results in the same order as input handles

## Usage Patterns

### Basic Workflow

```lua
function execute_workflow()
    -- Create timer
    wf.sleep("5s"):await()
    
    -- Execute activity
    local result = activities.hello_world("arg1", "arg2"):await()
    
    return result
end
```

### Race Pattern

```lua
local first = wf.race({
    activities.hello_world("Hello", "World"),
    wf.sleep("5s")
})
```

### Parallel Execution

```lua
local results = wf.parallel({
    activities.activity1(),
    activities.activity2()
})
```

### Command Handle

A wrapper around workflow commands that provides a consistent interface for handling responses and errors.

Methods:

- `await()`: Blocks until command completes and returns the result
- `response()`: Returns the command's response channel
- `error()`: Returns the command's error information

# Activity Results Guide

## Understanding Activity Results

Activities in temporal workflows can return various types of data. It's essential to understand and properly handle
these results to avoid common errors.

### Activity Return Types

Activities can return:

- Simple values (strings, numbers)
- Tables/objects
- Complex nested structures
- nil values

### Common Patterns

#### Table Results

Many activities return table structures for flexibility:

```lua
-- Activity implementation
function some_activity(input)
    return {
        status = "success",
        message = "Process completed",
        data = input
    }
end

-- Usage in workflow
local result = activities.some_activity("input"):await()
-- Access specific fields
local message = result.message
```

#### String Results

For simple text outputs:

```lua
-- Activity implementation
function simple_activity(input)
    return "Processed: " .. input
end

-- Usage in workflow
local result = activities.simple_activity("input"):await()
-- Result is directly usable as string
```

### Debugging Activity Results

#### Basic Result Inspection

```lua
function execute_workflow()
    local result = activities.some_activity():await()
    
    -- Print type and structure
    print("Result type:", type(result))
    if type(result) == "table" then
        for k, v in pairs(result) do
            print(k, "=", v)
        end
    end
end
```

#### Common Issues

1. **Type Mismatch**
    - Issue: Attempting string operations on tables
    - Solution: Check result type and extract needed fields
   ```lua
   local result = activity:await()
   local text = type(result) == "table" and result.message or tostring(result)
   ```

2. **Missing Fields**
    - Issue: Accessing non-existent table fields
    - Solution: Validate field existence
   ```lua
   local result = activity:await()
   if result and result.message then
       -- use result.message
   end
   ```

3. **Nil Results**
    - Issue: Activity returns nil unexpectedly
    - Solution: Add nil checks
   ```lua
   local result = activity:await() or {}
   ```
