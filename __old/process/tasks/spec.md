# Tasks Module Specification

The Tasks module provides functionality for managing concurrent task execution within a Lua environment. It enables
asynchronous task scheduling, execution, and communication through channels.

## Core Concepts

### Task Channel

Tasks are managed through a channel-based system where each task has:

- A unique identifier (TaskID)
- Input values
- A communication channel for results and updates
- Completion/failure status

### Lifecycle

1. Tasks are submitted through the task runner
2. The task handler receives tasks through an inbox channel
3. Tasks can send intermediate results, complete successfully, or fail with an error
4. The channel is closed after task completion/failure

## API Reference

### Tasks Module Functions

#### `tasks.channel([buffer_size])`

Creates a new channel for receiving tasks.

- **Parameters:**
    - `buffer_size` (number, optional): Size of channel buffer (default: 1)
- **Returns:** Yields a channel request
- **Example:**

```lua
local inbox = tasks.channel()  -- Create unbuffered channel
local inbox = tasks.channel(5) -- Create buffered channel
```

> Current generation of integration only creates channel once.

### Task Methods

#### `task:input()`

Retrieves the input values passed to the task.

- **Returns:** The original input values passed when executing the task
- **Example:**

```lua
local value = task:input()
print("Received input:", value)
```

#### `task:complete(...)`

Completes the task successfully with optional result values.

- **Parameters:**
    - `...` (any): Zero or more values to return as the task result
- **Example:**

```lua
task:complete("success", 100)
```

#### `task:fail(error_message)`

Fails the task with an error message.

- **Parameters:**
    - `error_message` (string): Error message describing the failure
- **Example:**

```lua
task:fail("Invalid input provided")
```

#### `task:send(...)`

Sends intermediate results during task execution.

- **Parameters:**
    - `...` (any): Values to send as intermediate results
- **Example:**

```lua
task:send("progress", 50)
```

## Task Handler Implementation

A task handler is a Lua function that:

1. Creates an inbox channel using `tasks.channel()`
2. Receives tasks in a loop using the channel
3. Processes tasks and sends results
4. Exits when the channel is closed

Example task handler:

```lua
function handler()
    local inbox = tasks.channel()
    
    while true do
        local task, ok = inbox:receive()
        if not ok then
            break -- Channel closed
        end
        
        -- Process task
        local input = task:input()
        
        -- Send progress updates
        task:send("progress", 50)
        
        -- Complete or fail
        if input.valid then
            task:complete("success")
        else
            task:fail("invalid input")
        end
    end
end
```

## Best Practices

### Error Handling

1. Always check channel receive status with `local task, ok = inbox:receive()`
2. Use `task:fail()` to report errors instead of raising Lua errors
3. Clean up resources before completing/failing tasks

### Task Results

1. Use `task:send()` for progress updates and intermediate results
2. Only call `task:complete()` or `task:fail()` once per task
3. Structure results consistently across similar tasks

### Channel Management

1. Use appropriate buffer size for expected workload
2. Process tasks in a timely manner to avoid blocking
3. Handle channel closure gracefully

### Resource Management

1. Clean up resources when tasks complete
2. Use coroutines for concurrent processing when needed
3. Avoid keeping unnecessary references to task objects

## Limitations and Considerations

1. Task handlers run in the same Lua state
2. Tasks are processed sequentially within a handler
3. Channel operations can block execution
4. Memory usage scales with number of pending tasks
5. Task results must be serializable Lua values

## Example Patterns

### Progress Reporting

```lua
function handler()
    local inbox = tasks.channel()
    while true do
        local task, ok = inbox:receive()
        if not ok then break end
        
        -- Send progress updates
        task:send("status", "starting")
        task:send("progress", 33)
        task:send("status", "processing")
        task:send("progress", 66)
        task:send("status", "finishing")
        task:send("progress", 100)
        
        task:complete("done")
    end
end
```

### Concurrent Processing

```lua
function handler()
    local inbox = tasks.channel(3)
    local completed = channel.new(3)
    
    -- Spawn worker coroutines
    for i = 1, 3 do
        coroutine.spawn(function()
            local task, ok = inbox:receive()
            if ok then
                process_task(task)
                completed:send(true)
            end
        end)
    end
    
    -- Wait for completion
    for i = 1, 3 do
        completed:receive()
    end
end
```

### Batch Processing

```lua
function handler()
    local inbox = tasks.channel()
    local batch = {}
    
    while true do
        local task, ok = inbox:receive()
        if not ok then break end
        
        table.insert(batch, task)
        if #batch >= 10 then
            process_batch(batch)
            batch = {}
        end
    end
    
    -- Process remaining batch
    if #batch > 0 then
        process_batch(batch)
    end
end
```