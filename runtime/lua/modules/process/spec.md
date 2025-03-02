# Process System Specification

## Overview

The process system provides isolated process management with message passing capabilities. The system is designed for
building distributed applications with strong isolation guarantees.

This api only works inside processes and workflows.

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

Messages can be sent through named topics, with an optional fallback to a default inbox channel:

```lua
-- Send messages 
process.send(pid_or_name, "topic", value1)    -- Single value
process.send(pid_or_name, "topic", value1, value2, value3)  -- Multiple values sent as separate messages
```

Each value sent is delivered as a separate message through the channel system. When sending multiple values, they are
delivered sequentially as individual messages, not as a batch.

### Receiving Messages

Messages can be received through two types of channels:

1. Named Topic Channel:

```lua
-- Listen on a specific topic
local msgs = process.listen("topic")
local value = msgs:receive()  -- Each receive gets one value
```

2. Default Inbox Channel (@inbox):

```lua
-- Listen for undelivered messages
local inbox = process.inbox()
local msg = inbox:receive()  -- Gets one message with metadata
```

#### Message Formats

There are two different message formats depending on how the message is received:

1. Named Topic Messages:
    - Each message contains a single value
    - Multiple values sent to a topic arrive as separate messages
   ```lua
   local msgs = process.listen("mytopic")
   local value = msgs:receive()  -- Single value per receive
   ```

2. Inbox Messages (@inbox):
    - Messages that fall back to @inbox are wrapped in a table with metadata
    - Each message contains:
        - topic: String of the original message topic
        - payload: Table containing the message value(s)
   ```lua
   local inbox = process.inbox()
   local msg = inbox:receive()  -- Gets {topic="topic", payload={value}} 
   ```

Example message handling:

```lua
-- Named topic handling
local msgs = process.listen("mytopic")
while true do
    local value = msgs:receive()  -- Each receive gets next value
    print("Got value:", value)
end

-- Inbox handling
local inbox = process.inbox()
while true do
    local msg = inbox:receive()
    print("Topic:", msg.topic)
    print("Value:", msg.payload[1])  -- Access first value in payload table
end
```

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
    local inbox = process.inbox()
    
    -- Main event loop
    while true do
        local result = channel.select({
            events:case_receive(),
            msgs:case_receive(),
            inbox:case_receive()
        })
        
        if result.channel == events then
            local event = result.value
            if event.event.kind == process.EVENT_CANCEL then
                -- Handle cancellation
                break
            end
        end
        
        -- Process topic messages
        if result.channel == msgs then
            local value = result.value
            -- Handle single value from topic
        end
        
        -- Process inbox messages
        if result.channel == inbox then
            local msg = result.value
            -- Handle inbox message with topic and payload
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
    - Each value is delivered as a separate message
    - Messages preserve send order between pairs
    - Buffered message delivery
    - Topic messages contain raw values
    - Inbox messages contain topic and payload metadata

3. Resource Management
    - Graceful process termination
    - Deadline-based cancellation
    - Automatic cleanup of terminated processes