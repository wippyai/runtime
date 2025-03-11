# Bubble Tea Commands in Lua

## Overview

This specification defines how Bubble Tea commands are represented and used in Lua. Commands are operations that can be
executed to perform various terminal manipulations and control function. Typically, commands received from updated
models.

## Command Structure

Commands are represented as userdata objects with an `execute()` method. When executed, commands return messages that
can be processed by the application.

## Creating Commands

### Basic Command Creation

Commands are typically accessed through the `btea.commands` table:

```lua
local cmd = btea.commands.clear_screen
```

### Command Composition

Commands can be composed using two primary methods:

1. `btea.batch(commands)`: Executes commands in parallel
2. `btea.sequence(commands)`: Executes commands in sequence

Example:

```lua
local batch_cmd = btea.batch({ 
    btea.commands.clear_screen,
    btea.commands.show_cursor 
})

local seq_cmd = btea.sequence({ 
    btea.commands.enter_alt_screen,
    btea.commands.hide_cursor 
})
```

## Available Commands

### Screen Management

- `clear_screen`: Clears the terminal screen
- `enter_alt_screen`: Switches to alternate screen buffer
- `exit_alt_screen`: Returns to main screen buffer

### Mouse Control

- `enable_mouse_cell_motion`: Enables mouse movement tracking by cell
- `enable_mouse_all_motion`: Enables continuous mouse movement tracking
- `disable_mouse`: Disables all mouse tracking

### Cursor Control

- `hide_cursor`: Makes the cursor invisible
- `show_cursor`: Makes the cursor visible

### Paste Mode

- `enable_bracketed_paste`: Enables bracketed paste mode
- `disable_bracketed_paste`: Disables bracketed paste mode

### Focus Reporting

- `enable_report_focus`: Enables focus event reporting
- `disable_report_focus`: Disables focus event reporting

### Window Management

- `set_window_title(title)`: Sets the terminal window title

```lua
local cmd = btea.commands.set_window_title("My Application")
```

- `window_size`: Requests current window dimensions

### Program Control

- `quit`: Exits the application
- `suspend`: Suspends the application

## Command Execution

Commands can be executed using the `execute()` method:

```lua
local msg = cmd:execute()
```

The execute method returns a message that can be processed by the application. The message structure follows the format
defined in the Messages specification.

## Best Practices

1. **Command Composition**: Use `batch` for parallel operations and `sequence` for ordered operations
2. **Error Handling**: Always check for nil or error returns from command execution
3. **Resource Management**: Commands that enter alternate modes (like alt screen or mouse tracking) should have
   corresponding exit commands
4. **Message Processing**: Handle command messages appropriately in your application's update loop

## Example Usage

```lua
-- Initialize application with multiple commands
local init_cmds = btea.sequence({
    btea.commands.enter_alt_screen,
    btea.commands.hide_cursor,
    btea.commands.enable_mouse_cell_motion,
    btea.commands.set_window_title("My TUI App")
})

-- Cleanup commands for application exit
local cleanup_cmds = btea.sequence({
    btea.commands.show_cursor,
    btea.commands.disable_mouse,
    btea.commands.exit_alt_screen
})

-- Execute commands
local msg = init_cmds:execute()
```

## Integration with Event Loop

Commands are typically processed within an event loop or message handler:

```lua
while true do
    local msg = cmd:execute()
    if msg.type == "update" then
        -- Handle command response
    end
end
```

## Error Handling

1. Commands that fail to execute may return nil or an error message
2. Always validate command composition inputs
3. Handle cleanup commands in a finally block or equivalent

## Command Lifecycle

1. **Creation**: Command is created through btea.commands or composition
2. **Queueing**: Command is optionally queued with other commands
3. **Execution**: Command is executed via execute()
4. **Response**: Message is returned and processed
5. **Cleanup**: Any necessary cleanup commands are executed

## Recommended Command Handling Pattern

The recommended approach for handling commands in a Bubble Tea application is to process them in a separate coroutine.
This pattern provides better separation of concerns and prevents blocking the main application loop.

### Command Processor Setup

```lua
-- Create channels for communication
local cmd_channel = channel.new(128)  -- Buffer size of 128
local done = channel.new()            -- Signal channel for cleanup

-- Command processor coroutine
coroutine.spawn(function()
    while true do
        -- Use channel.select to handle multiple cases
        local result = channel.select {
            cmd_channel:case_receive(), -- Handle incoming commands
            done:case_receive()         -- Handle cleanup signal
        }

        if result.channel == done then
            -- Exit command processor
            break
        else 
            -- Process command from cmd_channel
            local cmd = result.value
            if cmd then
                local msg = cmd:execute()
                if msg then
                    -- Send message upstream if needed
                    upstream.send(msg)
                end
            end
        end
    end
end)

-- Main application loop
while true do
    local task, ok = inbox:receive() -- todo: task channel is subject to change
    if not ok then
        -- Signal command processor to shut down
        done:send(true)
        break
    end

    local msg = task:input()
    if type(msg) == "table" and msg.type == "update" then
        -- Handle input updates
        local cmd = input:update(msg)
        if cmd then
            -- Send command to processor
            cmd_channel:send(cmd)
        end

        -- Complete task
        task:complete("ok")
    end
end

-- Cleanup
done:close()
```

### Key Components:

1. **Command Channel**: A buffered channel for sending commands to the processor
2. **Done Channel**: A signal channel for clean shutdown
3. **Processor Coroutine**: A separate coroutine that handles command execution
4. **Channel Select**: Uses `channel.select` for handling multiple channel cases
5. **Cleanup Handling**: Proper shutdown sequence for the command processor

### Benefits:

1. Non-blocking command execution
2. Clean separation of concerns
3. Proper resource cleanup
4. Scalable command handling
5. Better error isolation

### Example Usage with Batch Commands:

```lua
-- Create a batch of commands
local batch = btea.sequence({ 
    input:focus(),
    btea.commands.set_window_title("My Window")
})

-- Send to command processor
cmd_channel:send(batch)
```

## Notes

- Command execution yields until the operation completes
- Some commands may require specific terminal capabilities
- Window title setting may not work in all terminal emulators
- Always ensure proper channel cleanup on application exit