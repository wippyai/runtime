<!-- SPDX-License-Identifier: MPL-2.0 -->

# queue

Message queue operations for publishing and consuming messages. IO, nondeterministic.

## Loading

```lua
local queue = require("queue")
```

## Functions

### publish(queue_id: string, data: any, headers?: table) → boolean, error

Publishes a message to a queue.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| queue_id | string | yes | - | Queue identifier (format: "namespace:name") |
| data | any | yes | - | Message data (converted to payload) |
| headers | table | no | nil | Optional message headers |

**headers fields:**

Headers can be any key-value pairs where keys are strings and values are string, integer, number, or boolean.

Two header namespaces are recognized:

- **Neutral keys** (driver-agnostic): `priority`, `correlation_id`, `reply_to`, `encoding`, `schema`, `source`, `traceparent`, `tracestate`, `content_type`, `message_type`. Drivers that have a native equivalent translate these onto typed fields (e.g. AMQP `CorrelationId`, SQS system attributes); otherwise they pass through unchanged.
- **Driver-prefixed keys**: `amqp.*`, `sqs.*` (extensible to `kafka.*`, `jetstream.*`). The matching driver may consume some of them (e.g. AMQP consumes `amqp.mandatory` and `amqp.expiration`); any key the matching driver does not special-case, and every prefixed key on a non-matching driver, is carried through to the consumer verbatim. Publishers can therefore rely on every header they set being visible on the receive side.

Keys prefixed with `x_` (e.g. `x_original_queue`, `x_dead_letter_reason`, `x_dead_letter_time`, `attempts`) are reserved for DLQ/redelivery bookkeeping written by the driver and MUST NOT be set by publishers.

**Returns:**
- Success: `true, nil`
- Error: `nil, error` - error is structured (has `:kind()`, `:message()`)

**Errors (structured):**

| Condition | Kind | Retryable | Notes |
|-----------|------|-----------|-------|
| no context | errors.INVALID | no | |
| queue publishing not allowed | errors.INVALID | no | Security check failed |
| queue manager not found | errors.INVALID | no | |
| queue ID empty | errors.INVALID | no | |
| not allowed to publish to queue | errors.INVALID | no | Queue-specific security check |
| message data required | errors.INVALID | no | |
| message data cannot be empty | errors.INVALID | no | |
| publish operation fails | errors.INTERNAL | no | Queue manager error |

**Example:**

```lua
local ok, err = queue.publish("app.queue:tasks", {
    action = "process_order",
    order_id = 12345
})
if err then error(err) end

-- With headers
ok, err = queue.publish("app.queue:tasks", "simple message", {
    priority = 5,
    correlation_id = "abc-123"
})
```

### message() → Message, error

Retrieves the current message from the delivery context. Only available when processing queue messages (in consumer context).

**Returns:**
- Success: `Message` - message object with methods
- Error: `nil, error` - error is structured

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| no context | errors.INVALID | no |
| no delivery found in context | errors.INVALID | no |

**Example:**

```lua
local msg, err = queue.message()
if err then error(err) end

local id = msg:id()
local correlation_id = msg:header("correlation_id")
```

### info(queue_id: string) → table, error

Returns operational stats for a declared queue.

| Param | Type | Required | Notes |
|-------|------|----------|-------|
| queue_id | string | yes | Queue identifier (format: "namespace:name") |

**Returns:** `table, nil` where table may contain `message_count`, `consumer_count`, and `ready` keys (driver-dependent).

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| no context | errors.INVALID | no |
| queue manager not found | errors.INVALID | no |
| queue ID empty | errors.INVALID | no |
| queue not found | errors.INTERNAL | no |
| driver not found | errors.INTERNAL | no |
| driver get info fails | errors.INTERNAL | no |

## Types

### Message

Returned by `queue.message()`. Represents a queue message being processed.

**Lifetime:** a Message wrapper is only valid while the consumer handler is running. Once the handler returns, the underlying delivery is released back to the pool; subsequent method calls on a captured wrapper return an INVALID error (`queue.Message released`). Do not store references beyond handler scope.

**Settlement:** `ack` and `nack` are single-shot — whichever call lands first wins. The consumer runtime will also auto-ack on handler success and auto-nack on handler error, but will skip the implicit settle if the handler already claimed it. Subsequent settle attempts return an INVALID error (`queue.Message already settled`).

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| id | () | string, error | Message unique identifier |
| header | (key: string) | any, error | Get single header value, nil if not found |
| headers | () | table, error | Get all headers as table |
| ack | () | boolean, error | Acknowledge successful processing. Single-shot. |
| nack | () | boolean, error | Signal failure, request redelivery or dead-letter. Single-shot. |

#### msg:id() → string, error

Returns the message's unique identifier.

**Returns:** `string, nil` - message ID

**Example:**

```lua
local msg = queue.message()
local id = msg:id()
```

#### msg:header(key: string) → any, error

Returns a single header value by key.

| Param | Type | Required | Notes |
|-------|------|----------|-------|
| key | string | yes | Header key to retrieve |

**Returns:**
- `value, nil` - header value (string, number, or boolean) if exists
- `nil, nil` - if header doesn't exist

**Example:**

```lua
local msg = queue.message()
local priority = msg:header("priority")
local correlation_id = msg:header("correlation_id")
```

#### msg:headers() → table, error

Returns all message headers as a table.

**Returns:** `table, nil` - table with all headers (empty table if no headers)

**Example:**

```lua
local msg = queue.message()
local headers = msg:headers()
for k, v in pairs(headers) do
    print(k, v)
end
```

#### msg:ack() → boolean, error

Acknowledges successful processing. Single-shot: only one of `ack`/`nack` (including the consumer's post-handler auto-settle) can take effect per delivery.

**Returns:**
- Success: `true, nil`
- Error: `nil, error` - error is structured

**Errors (structured):**

| Condition | Kind | Retryable | Notes |
|-----------|------|-----------|-------|
| queue.Message released | errors.INVALID | no | Called after the handler returned |
| no context | errors.INVALID | no | |
| queue.Message already settled | errors.INVALID | no | Prior manual or auto settle won |
| driver ack fails | errors.INTERNAL | no | Broker-side error, caller ctx error if cancelled |

**Example:**

```lua
local msg = queue.message()
local ok, err = msg:ack()
if err then error(err) end
```

#### msg:nack() → boolean, error

Signals processing failure. Redelivery or dead-letter routing is driver-dependent. Single-shot (see `msg:ack`).

**Returns:**
- Success: `true, nil`
- Error: `nil, error` - error is structured

**Errors (structured):** same table as `msg:ack`.

**Example:**

```lua
local msg = queue.message()
local ok, err = msg:nack()
if err then error(err) end
```

## Errors

This module returns structured errors. Check kind with `errors.*` constants:

```lua
local ok, err = queue.publish("app.queue:tasks", data)
if err then
    if err:kind() == errors.INVALID then
        -- bad input or security violation
    elseif err:kind() == errors.INTERNAL then
        -- queue manager error
    end
end
```

**Possible kinds:** `errors.INVALID`, `errors.INTERNAL`

## Example

```lua
local queue = require("queue")
local logger = require("logger")

-- Publishing a message
local ok, err = queue.publish("app.queue:tasks", {
    action = "send_email",
    user_id = 456,
    template = "welcome"
}, {
    priority = 10,
    correlation_id = "req-789"
})

if err then
    logger:error("publish failed", {error = tostring(err)})
    return
end

-- Consuming a message (in consumer context)
local msg, err = queue.message()
if err then
    logger:error("failed to get message", {error = tostring(err)})
    return
end

local msg_id = msg:id()
local priority = msg:header("priority")
local all_headers = msg:headers()

logger:info("processing message", {
    msg_id = msg_id,
    priority = priority,
    headers = all_headers
})
```
