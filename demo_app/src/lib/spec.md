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