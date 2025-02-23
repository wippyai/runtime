# Process System Specification

## Overview

The process system provides isolated process management with message passing capabilities. The system is designed for
building distributed applications with strong isolation guarantees.

## Process Properties and Structure

### Process Identification (PID)

Each process has a unique PID with the following components:

- Node (optional) - Physical node identifier
- Host (required) - Host identifier that determines process behavior and lifecycle
- Registry ID (required) - Composite namespace and name identifier
    - Namespace - Process namespace
    - Name - Name within namespace
- Process Name (required) - Unique instance identifier

PID String Format:

```
{node@host|namespace:name|procname}     // With optional node
{host|namespace:name|procname}          // Without node
```

Examples:

```
{host1|app:worker|proc1}                // Local process
{node1@host1|app:worker|proc1}          // Process on remote node
```

### Process Information

Processes can access their own information:

```lua
-- Get process information
local pid = process.pid()        -- Get current process PID
local info = process.info()      -- Get process details
local args = process.input() -- Get initialization arguments
```

The info table contains:

- node: Node identifier (if present)
- host: Host identifier
- registry_id: Table containing namespace and name
- uniq_id: Unique process identifier
- start_time: Process start timestamp
- trap_exits: Boolean flag for exit signal handling

## Communication

### Message Passing

Messages are sent through named topics with an optional fallback to a default inbox:

```lua
-- Send messages
process.send(pid_or_name, "topic", message, [message2, ...])

-- Listen for messages on a specific topic
local msgs = process.listen("topic")
local value = msgs:receive()

-- Listen for undelivered messages
local inbox = process.inbox()
local msg = inbox:receive()
print(msg.topic)    -- Original topic
print(msg.payload)  -- Table containing message values
```

Key properties:

- Messages are asynchronous and non-blocking
- Topics starting with "@" are reserved for system use
- Messages fallback to @inbox if:
    - The target topic doesn't exist
    - The target process exists but isn't listening on the topic
- Inbox messages are structured with:
    - topic: Original message topic
    - payload: Table containing all message values
- Order is preserved between specific sender-receiver pairs
- Messages are buffered until received

### System Events

Processes can listen for system events:

```lua
local events = process.events()

-- Event types
process.EVENT_CANCEL  -- Process cancellation request
process.EVENT_RESULT  -- Process completion/failure
```

## Process Management

### Process Creation

```lua
-- Basic process spawning
local pid = process.spawn(registry_id, host_id, [args_table])

-- Spawn with monitoring
local pid = process.spawn_monitored(registry_id, host_id, [args_table])
```

### Process Lifecycle Control

```lua
-- Terminate a process
process.terminate(pid)

-- Request cancellation with deadline
process.cancel(pid, deadline)  -- deadline can be duration string or milliseconds
```

## Process Implementation

Processes are implemented as Lua modules that export a primary function:

```lua
local function run(args)
    -- Process initialization
    local pid = process.pid()
    
    -- Set up channels
    local events = process.events()
    local msgs = process.listen("messages")
    
    -- Main event loop
    while true do
        local result = channel.select({
            events:case_receive(),
            msgs:case_receive()
        })
        
        if result.channel == events then
            local event = result.value
            if event.event.kind == process.EVENT_CANCEL then
                -- Handle cancellation
                break
            end
        end
        
        -- Process messages
        if result.channel == msgs then
            -- Handle message
        end
    end
    
    -- Return result
    return { status = "completed" }
end

return { run = run }
```

## Configuration

Processes are configured through YAML entries:

```yaml
- name: process_name
  kind: process.lua
  meta:
    comment: "Process description"
  source: file://source.lua
  method: run
  modules: [ "time", "json" ]  # Required modules
```

### Root-Level Supervised Processes

The system can be configured to automatically launch and supervise certain processes at the root level. These are
processes that need to be always running and managed by the system itself:

```yaml
- name: service_name
  kind: process.service
  process: process_name    # Reference to the process definition
  host: system:heap       # Host where the process will run
  lifecycle:
    auto_start: true      # Start automatically with system
    restart: # Restart policy
      initial_delay: 5s
      max_attempts: 3
    depends_on: # Service dependencies
      - system:heap
```

Note: This is different from regular processes that might run for a long time - this configuration specifically tells
the system to treat the process as a supervised system service that should be maintained at the root level.

## Key Guarantees

1. Process Isolation
    - No shared memory between processes
    - Communication only through message passing
    - Strong isolation boundaries

2. Message Handling
    - Non-blocking send operations
    - Preserved message order between pairs
    - Buffered message delivery

3. Resource Management
    - Graceful process termination
    - Deadline-based cancellation
    - Automatic cleanup of terminated processes