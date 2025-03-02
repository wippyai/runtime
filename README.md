# Wippy Runtime

A distributed process system for building resilient AI applications with Erlang-style process isolation and supervision.

## Core Features

### Process System

- Erlang-style process isolation with unique PIDs (node_id@host:app_id:namespace.name:type.pid)
- Process types: Temporal (workflow-based), Ephemeral (temporary), and Persisted (with local storage)
- Built-in process lifecycle management with graceful upgrades and migrations
- Process registry for name-based addressing
- Bidirectional process linking and unidirectional monitoring
- Dynamic code modification and hot-swapping
- Security policies for process and function access control

### Communication

- Message-passing through system ports with guaranteed ordering between sender-receiver pairs
- Go-style channels for coroutine communication within processes
- Process groups for broadcast messaging and membership management
- Select operations for handling multiple channels/ports simultaneously

### Supervision & Recovery

- Hierarchical process supervision
- Automatic failure propagation through process links
- Configurable process priorities and exit signal handling
- State preservation across process migrations

### Development Tools

- Lua-based process definitions
- Built-in debugging capabilities for channels and processes
- System events monitoring for upgrades and migrations

## Example Usage

```lua
-- Define a worker process
local input = process:port("input"):channel()
local events = process:events()

-- Join worker group
local workers = process:group("workers")
workers:join()

-- Configure process
process.set_flags({
    trap_exits = true,
    priority = 75
})

-- Process loop
while true do
    local result = channel.select{
        input:case_receive(),
        events:case_receive()
    }
    -- Handle messages
end
```

## Key Properties

- Memory and crash isolation between processes
- Message-based communication only
- Preserved message ordering per sender-receiver pair
- Automatic message buffering
- Built-in process migration support
- Local process registry

## Requirements

- Go runtime environment, single binary
- Temporal.io for workflow processes (optional)

## Documentation

For detailed specifications and API documentation, see:

- [Process System Specification](./spec.md)
- [Channel System Documentation](./channels.md)