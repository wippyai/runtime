# Process System Specification

## Overview

The process system provides Erlang-style process isolation and supervision capabilities with runtime integration through
system ports. The system is designed for building distributed AI agent applications with strong isolation guarantees.

Current specification describes single node wide process system. Future versions will include distributed process
system and mesh networking capabilities.

## Process Properties

```lua
-- Static process properties
local pid = process.self        -- Current process ID
local info = process.info       -- Process information (PID, registered name, etc.)
local args = process.args       -- Process start arguments, includes previous state during upgrades/migration
```

## Process Identifiers (PIDs)

PIDs uniquely identify processes within a runtime session. They follow the format:

```
node_id@host:app_id:namespace.name:type.pid
```

Components:

- Static part (process location):
    - `node_id@host`: Physical node identifier and hostname where process runs (e.g., "node1@localhost")
    - `app_id`: Application identifier (e.g., "myapp")
    - `namespace.name`: Dot-separated path identifying process location and name (e.g., "workers.processor")
- Dynamic part:
    - `type` - Process type indicator:
        - 't' - Temporal (uses Temporal.io engine)
        - 'e' - ephemeral (default, temporary, short-lived)
        - 'p' - persisted (local storage)
    - `pid` - Global unique process identifier

Examples:

```lua
"node1@localhost:myapp:workers.processor:t.1234"    -- Temporal process (first version)
"node1@localhost:myapp:workers.cache:e.5678"        -- Ephemeral process
"node2@remote:myapp:workers.state:p.9012"          -- Persisted process
```

## Process Lifecycle Management

```lua
-- Process completion
process.complete(result)     -- Normal completion with result, result delivered to all monitors, process exits
process.fail(error)          -- Fail with error, error delivered to all monitors and linked processes, causes cascade failure, process exits

-- Process migration/upgrade (required for hot updates, read migration protocols)
process.upgrade(new_state)   -- Migrate process state if any, method called when source code has to be updated, no link or monitor messages are sent. Old process is terminated.

-- Process flags and priority
process.set_flags({
    trap_exits = true,      -- Convert exit signals to messages, default false
    priority = 50           -- Process priority (0-100, default 50)
})
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

> You can send messages based on alias names or PIDs.

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

> Port only visible to system when being :receive()'d.

### Event Message Formats

```lua
-- Migration event
{
    type = "migrate",
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
    pid = "node1@localhost:app1:workers.processor:p.1234"
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
local monitor_ch = process.monitor("worker")       
child2:unmonitor("worker")
```

> In addition to system monitors channel, per process monitor will include completion or failure payloads. Use it to
> get result of execution. You can create links/monitors to any process using name alias or PID.

### Atomic shortcuts

```lua
-- Spawn and link
local child = process:spawn_link("worker")

-- Spawn and monitor
local child = process:spawn_monitor("worker")
```

### Exit Events (Links) and Down Events (Monitors)

```lua
{
    pid = "node1@localhost:app1:namespace.worker:t.1234",
    reason = "error message or normal",
    error = "optional error details"
}
```

### Completion and Failure Handling

```lua
{
    pid = "node1@localhost:app1:namespace.worker:t.1234",
    result = {"any payload shape"}   
}
```

## Process Registry

The registry system allows processes to be registered under names for easy lookup.

```lua
-- Registration and unregistration
process.register("my_name") -- local registration
process.unregister("my_name") -- local unregistration

-- Lookup
local pid = process.whereis("my_name")           -- Get PID by name
```

> Registration is local to the application level and not persistent across restarts.

## Example Process

```lua
-- System ports
local input_ch = process:port("input")
local events_ch = process:events()
local exits_ch = process:exits()
local downs_ch = process:downs()

-- Group membership
local workers = process:group("workers")
workers:join()

local group_events_ch = workers:events()

-- Enable exit signal handling and set priority
process.set_flags({
    trap_exits = true,
    priority = 75  -- Higher priority process
})

-- Main loop
while true do
    local result = channel.select{
        input_ch:case_receive(),
        events_ch:case_receive(),
        exits_ch:case_receive(),
        downs_ch:case_receive(),
        group_events_ch:case_receive()
    }
    
    if result.channel == input_ch then
        -- Handle system input
        if result.value.error then
            print("Error received:", result.value.error)
        else
            -- Process normal input
            child1.send("input", "Hello, World!")
        end
        
    elseif result.channel == events_ch then
        -- Handle system events (e.g., migration)
        local event = result.value
        if event.type == "migrate" then         
            -- if you have no state simply confirm migration
            process.migrate()
        end
        
    elseif result.channel == exits_ch then
        -- Handle linked process exit
        local exit = result.value
        print("Linked process exit:", exit.pid, exit.reason)
        
    elseif result.channel == downs_ch then
        -- Handle monitored process termination
        local down = result.value
        print("Monitored process down:", down.pid, down.reason)
        
    elseif result.channel == group_events_ch then
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

### Carrying state across migrations

```lua
-- Process state
local state = {counter=0}

-- to migrate process state
process.migrate(state)
```

> Process migration is equivalent to process starting new version of process with state as args. PID remains same.

## Process Export Pattern

Process files must export process functions that create process definitions. Args will be passed to this function on
start.

The process name will be constructed from the definition name and export key via `.`. Make sure to use proper naming conventions.

### Example Structure

```lua
-- definition: processes.worker
local function create()
    -- Private state in closure
    local state = {}
    
    return {
        init = function(args) end,
        handle = function(port, msg) end,
        terminate = function(reason) end
    }
end

return {create = create}  -- Available as "processes.worker.create"
```

## Key Properties and Guarantees

1. Process Isolation
    - Processes cannot share memory
    - Communication only through message passing
    - System ports are unidirectional (read-only from process perspective)

2. Message Passing Properties:
    - Send operations never block the sender
    - The message order is preserved only between a specific sender-receiver pair
    - Messages are buffered externally until the receiving process has buffer space

3. Process Links
    - Links are always bidirectional
    - Exit signals propagate through links
    - Must trap_exits to handle link failures as messages

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
    - Previous state available in process args (sent via `process.migrate`)
    - Migration failure treated as process failure