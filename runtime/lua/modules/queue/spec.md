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
| message data missing | errors.INVALID | no | |
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

## Types

### Message

Returned by `queue.message()`. Represents a queue message being processed.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| id | () | string, error | Message unique identifier |
| header | (key: string) | any, error | Get single header value, nil if not found |
| headers | () | table, error | Get all headers as table |

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
