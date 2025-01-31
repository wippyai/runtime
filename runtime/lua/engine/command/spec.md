# Command Layer Specification for Pony Coroutine VM

## Overview

The Command Layer provides an asynchronous command execution system for Pony processes, building on top of the channel
system to enable non-blocking command operations. It allows processes to schedule commands and receive their results
through a consistent interface.

## Core Concepts

### Command

A command represents an asynchronous operation with the following properties:

- Type: String identifier for the command operation
- Parameters: Optional list of Lua values passed to the command
- Response Channel: Dedicated channel for receiving the command's result
- State: Tracks completion status and result/error information

### Command Layer

The command layer manages:

- Command scheduling and execution
- Result/error distribution
- Integration with the Pony VM's execution model

## Usage in Lua

### Creating Commands

```lua
-- Create a new command
local cmd = command.new("command_type", {...optional params...})

-- Get response channel for result
local resp = cmd:response()
```

### Command Methods

Commands provide the following methods:

- `response()`: Returns the channel for receiving the command's result
- `is_complete()`: Returns true if command has finished (success or failure)
- `is_canceled()`: Returns true if command was canceled
- `error()`: Returns error message if command failed, nil otherwise
- `result()`: Returns (result, error) tuple - blocks until completion

### Command States

A command can be in one of these states:

1. Pending: Initial state after creation
2. Completed: Successfully finished with a result
3. Failed: Completed with an error
4. Canceled: Terminated before completion

### Using with Select

Commands integrate with the channel select system:

```lua
-- Wait for multiple command results
local result = channel.select{
    cmd1:response():case_receive(),
    cmd2:response():case_receive()
}

if result.channel == cmd1:response() then
    -- Handle cmd1 result
else
    -- Handle cmd2 result
end
```

## Implementation Details

### Command Creation

1. Each command gets a unique response channel
2. Channel name format: `cmd.<type>.<counter>`
3. Commands are automatically scheduled on creation
4. Command parameters are preserved for execution

### Command Processing

The command layer follows these steps:

1. Commands enter pending queue on creation
2. Layer processes pending commands during VM step
3. Results/errors are queued for distribution
4. Results sent through response channels
5. Channels are closed after sending

### Error Handling

Error scenarios are handled as follows:

1. Command errors are propagated through response channels
2. Channel closure indicates command completion
3. Error status can be checked via command methods
4. Canceled commands receive special error type

### Threading Model

Important threading considerations:

1. Command operations are coroutine-safe
2. Layer maintains thread-safe state with mutex
3. Command context bound to VM execution
4. Response channels follow standard channel rules

## Best Practices

1. **Command Creation**
    - Use descriptive command types
    - Keep parameters serializable
    - Check command creation errors

2. **Result Handling**
    - Always check for errors
    - Use select for multiple commands
    - Handle cancellation gracefully

3. **Resource Management**
    - Don't leave commands unprocessed
    - Clean up after command completion
    - Monitor command completion state

4. **Error Handling**
    - Check command errors explicitly
    - Handle cancellation scenarios
    - Provide meaningful error context

## Limitations and Considerations

1. **Execution Context**
    - Commands only work in Pony processes
    - Must have command layer in context
    - Response channels follow channel limits

2. **Resource Usage**
    - Each command creates a channel
    - Commands persist until completed
    - Consider command lifecycle

3. **Cancellation**
    - Commands can't be un-canceled
    - Cancellation is immediate
    - Resources freed on cancel

## Integration with VM

The command layer integrates with the VM:

1. Registered as VM execution layer
2. Processes commands during VM steps
3. Maintains command context state
4. Coordinates with channel layer

## Example Patterns

### Basic Command Execution

```lua
-- Create and execute command
local cmd = command.new("fetch_data", {id = "123"})
local resp = cmd:response()
local result, ok = resp:receive()

if ok then
    -- Handle successful result
else
    -- Handle error case
end
```

### Multiple Command Processing

```lua
-- Process multiple commands
local cmd1 = command.new("operation1")
local cmd2 = command.new("operation2")

-- Wait for either result
local result = channel.select{
    cmd1:response():case_receive(),
    cmd2:response():case_receive()
}

-- Process result based on source
if result.channel == cmd1:response() then
    handle_result1(result.value)
else
    handle_result2(result.value)
end
```

### Error Handling Pattern

```lua
local cmd = command.new("risky_operation")

-- Check completion status
if cmd:is_complete() then
    if cmd:error() then
        -- Handle error case
    else
        local result = cmd:result()
        -- Process result
    end
end
```

## Testing and Debugging

The command layer provides testing support:

1. Command state inspection
2. Error injection capabilities
3. Cancellation testing
4. Integration test helpers

### Example Test Cases

```lua
-- Test command completion
local cmd = command.new("test")
assert(not cmd:is_complete())
-- Process command
assert(cmd:is_complete())
assert(cmd:result() ~= nil)

-- Test error handling
local cmd_err = command.new("test_error")
cmd_err:set_error("test error")
assert(cmd_err:error() ~= nil)

-- Test cancellation
local cmd_cancel = command.new("test_cancel")
cmd_cancel:cancel()
assert(cmd_cancel:is_canceled())
```