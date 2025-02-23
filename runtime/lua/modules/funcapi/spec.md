# Function API Specification

## Overview

The Function API provides a lightweight interface for function-based operations within the Pony runtime. Functions are
designed for shorter-lived operations with simple communication patterns.

## Core Concepts

### Function Identity

Each function has a unique Process ID (PID) that identifies it within the system. Functions use the following PID
format:

```
{node@host|namespace:name|procname}     // With optional node
{host|namespace:name|procname}          // Without node
```

### Communication Model

Functions support a streamlined messaging model:

- Direct message sending to other functions or processes
- Inbox-based message receiving

## API Reference

### Identity Operations

#### `func.pid()`

Returns the current function's PID as a string.

```lua
local my_pid = func.pid()
```

### Message Operations

#### `func.send(pid, topic, ...values)`

Sends one or more messages to a target PID on a specified topic.

Parameters:

- `pid` (string): Target PID in string format
- `topic` (string): Message topic (cannot start with '@')
- `...values`: One or more values to send as separate messages

Returns:

- `true` on success
- `nil, error_message` on failure

Example:

```lua
-- Send single message
func.send(target_pid, "notification", "Hello")

-- Send multiple messages
func.send(target_pid, "data", value1, value2, value3)
```

Constraints:

- Cannot send to topics starting with '@' (system reserved)
- Each value is sent as a separate message

#### `func.inbox()`

Creates or returns an existing inbox channel for receiving messages.

Returns:

- Channel object for receiving messages
- Messages are delivered as tables with format:
  ```lua
  {
    topic = "original_topic",
    payload = message_value
  }
  ```

Example:

```lua
local inbox = func.inbox()
local result = inbox:receive()  -- Blocks until message received
print(result.topic, result.payload)
```

Notes:

- Inbox is buffered and preserves message order
- Messages include original topic and payload
- Channel is automatically cleaned up when function completes

## Configuration

Functions are configured in YAML with the following structure:

```yaml
- name: function_name
  kind: function.lua
  meta:
    comment: "Function description"
  source: file://source.lua
  method: handler
  modules: [ "module1", "module2" ]  # Required modules
  pool:
    size: N  # Optional worker pool size
```

## Implementation Notes

1. Message Handling
    - Non-blocking sends
    - Buffered message delivery
    - Message order preservation between sender/receiver pairs
    - Automatic cleanup of channels on function completion

2. Error Handling
    - All operations return clear error messages on failure
    - Transcoding errors for payloads are logged but don't crash the function

3. Resource Management
    - Automatic cleanup of resources when function completes
    - Proper closure of channels and subscriptions

4. Context Requirements
    - Functions must have proper context with:
        - Function context
        - Node context
        - Unit of Work context
        - Transcoder context