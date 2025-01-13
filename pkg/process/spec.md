# Process System Specification

## Overview

The process system provides Erlang-style process isolation and supervision capabilities with runtime integration through
system ports. The system is designed for building distributed AI agent applications with strong isolation guarantees.

## Process Properties

```lua
-- Static process properties
local pid = process.self()      -- Current process ID
local info = process.info()     -- Process information (PID, registered name, etc.)
local args = process.args       -- Process start arguments, includes previous state during upgrades/migration
```

## Process Identifiers (PIDs)

PIDs uniquely identify processes within a runtime session. They follow the format:

```
node_id:app_id:namespace.name:pid.seq
```

Components:

- Static part (process location):
    - `node_id`: Physical node identifier where process runs (e.g., "node1")
    - `app_id`: Application identifier (e.g., "myapp")
    - `namespace.name`: Dot-separated path identifying process location and name (e.g., "workers.processor")
- Dynamic part:
    - `pid` - Global unique process identifier
    - `seq` - Process version/restart sequence number

Examples:

```lua
"node1:myapp:workers.processor:1234.1"    -- First version of process
"node1:myapp:workers.processor:1234.2"    -- Second version (after restart)
"node2:otherapp:service.endpoint:5678.1"  -- Process on different node
```

## Process Lifecycle Management

```lua
-- Process completion
process.complete(result)     -- Normal completion with result, result delivered to all monitors
process.fail(error)          -- Fail with error, error delivered to all monitors and linked processes, causes cascade failure

-- Process migration/upgrade (required for hot updates, read migration protocols)
process.upgrade(new_state)   -- Migrate process state if any, method called when source code has to be updated, no link or monitor messages are sent. Old process is terminated.
```

## Message Sending

```lua
-- Send one or more messages to a process port
process.send(pid_or_name, port_name, message, message2, ...)  -- Non-blocking, no return value

-- Examples
process.send("worker1", "input", {type="task", data="..."})
process.send(child_pid, "input",      -- Send multiple messages 
    {type="task1", data="..."},
    {type="task2", data="..."}
)
```

## System Integration

Ports are external communication channels that can only be written to from outside the process. From the process's
perspective, they behave as read-only channels.

```lua
-- Create system port for receiving input
local input = process:port("input")      -- System input port
local input = process:port("input", 10)  -- Input buffer size of 10 messages

-- System events channel for upgrades/migration, etc.
local events = process:events()
```

### Event Message Formats

```lua
-- Migration/upgrade event
{
    type = "upgrade",
    version = "1.2.3",
    deadline = timestamp
}
```

## Process Groups

Groups are implemented as special system processes that manage membership and group communication. Groups provide
mechanisms for broadcasting messages and managing distributed process collections.

```lua
-- Create or get group handle
local group = process:group("worker_pool")

-- Group membership
group:join()              -- Join current process to group
group:leave()             -- Leave group

local members = group:members()  -- Get list of current member PIDs

-- Group events
local events = group:events()    -- Get group events channel
-- Events format:
{
    type = "join"|"leave",
    pid = "node1:app1:workers.processor:1234.1"
}

-- Group messaging
group:broadcast("input", message)  -- Send message to all members' input port
```

## Process Management and Supervision

```lua
-- Spawn new processes
local child, err = process:spawn("worker")                -- Basic spawn
local child2, err = process:spawn("worker", {port=8080})  -- With args

-- Link processes (bidirectional failure propagation)
process.link("other_worker")    
child:unlink("other_worker")                 

-- Monitor processes (unidirectional monitoring)
process.monitor("worker")        
child2:unmonitor("worker")                  

-- Control exit signal handling
process.set_flags({trap_exits = true})   -- Convert exit signals to messages
```

### Exit Events (Links)

```lua
{
    pid = "node1:app1:namespace.worker:.1",
    reason = "error message or normal",
    error = "optional error details"
}
```

### Down Events (Monitors)

```lua
{
    pid = "node1:app1:namespace.worker:.1",
    reason = "error message or normal",
    error = "optional error details"
}
```

## Process Registry

The registry system allows processes to be registered under names for easy lookup.

```lua
-- Registration and unregistration
process.register("my_name")
process.unregister("my_name")

-- Lookup
local pid = process.whereis("my_name")           -- Get PID by name
```

## Example Process

```lua
-- System ports
local input = process:port("input")
local events = process:events()
local exits = process:exits()
local downs = process:downs()

-- Group membership
local workers = process:group("workers")
workers:join()
local group_events = workers:events()

-- Enable exit signal handling
process.trap_exits(true)

-- Main loop
while true do
    local result = channel.select{
        input:case_receive(),
        events:case_receive(),
        exits:case_receive(),
        downs:case_receive(),
        group_events:case_receive()
    }
    
    if result.channel == input then
        -- Handle system input
        if result.value.error then
            print("Error received:", result.value.error)
        else
            -- Process normal input
            child1.send("input", "Hello, World!")
        end
        
    elseif result.channel == events then
        -- Handle system events (e.g., migration)
        local event = result.value
        if event.type == "upgrade" then
            -- Prepare new state and migrate
            local new_state = transform_state(current_state)
            process.migrate(new_state)
        end
        
    elseif result.channel == exits then
        -- Handle linked process exit
        local exit = result.value
        print("Linked process exit:", exit.pid, exit.reason)
        
    elseif result.channel == downs then
        -- Handle monitored process termination
        local down = result.value
        print("Monitored process down:", down.pid, down.reason)
        
    elseif result.channel == group_events then
        -- Handle group membership changes
        local event = result.value
        if event.type == "join" then
            print("New worker joined:", event.pid)
        elseif event.type == "leave" then
            print("Worker left:", event.pid)
        end
    end
end
```

## Key Properties and Guarantees

1. Process Isolation
    - Processes cannot share memory
    - Communication only through message passing
    - System ports are unidirectional (read-only from process perspective)

2. Message Delivery
    - Message sending is always non-blocking
    - No delivery guarantees provided by send operation
    - Messages stay outside process until buffer space available

3. Process Links
    - Links are always bidirectional
    - Exit signals propagate through links
    - Must trap exits to handle link failures as messages

4. Process Monitors
    - Monitors are unidirectional
    - Down signals do not affect monitoring process
    - Monitors must be explicitly cleaned up

5. Process Registry
    - Names must be unique
    - Registration not persistent across restarts
    - Any process can register/unregister any other process

6. Process Migration
    - State preserved across migrations
    - Previous state available in process args
    - Migration failure treated as process failure

7. Process Groups
    - Groups implemented as system processes
    - Membership changes broadcast to all members
    - Multiple group membership supported
    - Groups can have properties and subgroups
    - Group operations are asynchronous