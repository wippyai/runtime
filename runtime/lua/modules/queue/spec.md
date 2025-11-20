# Lua Queue Module Specification

## Overview

The `queue` module provides functionality to publish messages to queues and retrieve incoming messages from the delivery context.

## Module Interface

### Module Loading

```lua
local queue = require("queue")
```

### Functions

#### queue.publish(queue_id: string, data: any, headers?: table)

Publishes a message to a queue.

Parameters:
- `queue_id`: Queue identifier string (e.g., "my:queue")
- `data`: Message payload (any Lua value)
- `headers`: Optional table of message headers (key-value pairs)

Returns:
- `true`: On successful publish (or nil on error)
- `error`: Error message (or nil on success)

#### queue.message()

Retrieves the current message from the delivery context.

Returns:
- `message`: Queue message object (or nil if no delivery context)
- `error`: Error message (or nil on success)

Notes:
- Only available within queue handler/consumer functions
- Returns the incoming message being processed

### Message Object

Message objects retrieved by `queue.message()` have the following methods:

#### message:id()

Returns the message ID.

Returns:
- `id`: String message identifier
- `error`: nil

#### message:header(key: string)

Gets the value of a specific header.

Parameters:
- `key`: Header key name

Returns:
- `value`: Header value (or nil if not found)
- `error`: nil

#### message:headers()

Gets all message headers as a table.

Returns:
- `headers`: Table containing all message headers
- `error`: nil

### Standard Header Constants

The following standard headers are commonly used:

- `timestamp`: Message creation timestamp (Unix seconds, auto-set)
- `delivery_count`: Number of delivery attempts
- `priority`: Message priority (0-9, higher = more important)
- `ttl`: Time to live in seconds
- `correlation_id`: For request-response correlation
- `reply_to`: Queue name for responses
- `content_type`: MIME type of body
- `message_type`: Application-specific message type
- `traceparent`: W3C trace context
- `tracestate`: W3C trace state

## Example Usage

```lua
local queue = require("queue")

-- Simple message publish
local ok, err = queue.publish("tasks:high", {
  task = "process_data",
  priority = 10
})

if err then
  print("Publish error:", err)
  return
end

-- Publish with custom headers
ok, err = queue.publish("tasks:email",
  {to = "user@example.com", subject = "Welcome"},
  {
    correlation_id = "req-12345",
    priority = 5,
    content_type = "application/json"
  }
)

-- In a queue consumer function, retrieve the incoming message
local msg, err = queue.message()
if err then
  print("Error:", err)
  return
end

-- Access message properties
print("Message ID:", msg:id())
print("Correlation ID:", msg:header("correlation_id"))
print("Timestamp:", msg:header("timestamp"))

-- Get all headers
local headers, err = msg:headers()
if not err then
  for k, v in pairs(headers) do
    print("Header:", k, "=", v)
  end
end
```

## Notes

- Queue must be declared before publishing messages
- Message data is automatically serialized to the queue's configured format
- Headers are optional metadata for routing, correlation, and tracing
- `timestamp` header is automatically added on message creation
- The queue manager must be available in the execution context
- `queue.message()` only works within queue handler execution context
