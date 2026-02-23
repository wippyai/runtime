<!-- SPDX-License-Identifier: MPL-2.0 -->

# events

Event bus subscribe and send operations. IO, nondeterministic.

## Loading

```lua
local events = require("events")
```

## Dependencies

### channel (from engine)

Subscriptions return channels for receiving events.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| receive | () | value, ok: boolean | Blocks until value available. ok=false when closed |
| case_receive | () | case | Creates receive case for channel.select |

See: `runtime/lua/engine/spec.md`

### Subscription

Returned by `events.subscribe()`.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| channel | () | channel | Returns channel for receiving events |
| close | () | boolean | Unsubscribes and closes channel. Returns true |

## Functions

### subscribe(system: string, kind?: string) -> Subscription, error

Subscribes to events from the event bus. Returns a subscription object with a channel for receiving matching events.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| system | string | yes | - | System pattern to match (supports wildcards like "test.*") |
| kind | string | no | nil | Optional kind filter. If nil, receives all kinds |

**Returns:**
- Success: `Subscription, nil` - subscription object with channel
- Error: `nil, error` - error is structured (has `:kind()`, `:message()`)

**Yields:** until subscription is established

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| system empty or nil | errors.INVALID | no |
| security policy denies subscription | errors.INVALID | no |
| no process context | errors.INTERNAL | no |

**Usage:**

```lua
local sub, err = events.subscribe("myapp.*")
if err then error(err) end

local ch = sub:channel()

-- With kind filter
local sub2, err = events.subscribe("myapp.system", "user.created")
if err then error(err) end
```

### send(system: string, kind: string, path: string, data?: any) -> boolean, error

Sends an event to the event bus. Event is delivered to all matching subscribers.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| system | string | yes | - | System identifier for the event |
| kind | string | yes | - | Event kind/type |
| path | string | yes | - | Event path (used for routing/filtering) |
| data | any | no | nil | Optional event payload (table, string, number, etc.) |

**Returns:**
- Success: `true, nil`
- Error: `nil, error` - error is structured (has `:kind()`, `:message()`)

**Yields:** until event is sent

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| system empty or nil | errors.INVALID | no |
| kind empty or nil | errors.INVALID | no |
| path empty or nil | errors.INVALID | no |
| security policy denies send | errors.INVALID | no |

**Usage:**

```lua
local ok, err = events.send("myapp.system", "user.created", "/users/123")
if err then error(err) end

-- With data payload
local ok, err = events.send("myapp.system", "order.placed", "/orders/456", {
    user_id = 123,
    amount = 99.99,
    items = {"item1", "item2"}
})
if err then error(err) end
```

## Types

### Subscription

Returned by `events.subscribe()`. Manages event subscription lifecycle.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| channel | () | channel | Returns channel for receiving events. Same channel on subsequent calls |
| close | () | boolean | Unsubscribes from bus and closes channel. Returns true. Safe to call multiple times |

#### sub:channel() -> channel

Returns the channel for receiving events. First call returns the channel, subsequent calls return the same channel.

**Returns:** channel userdata

**Received event structure:**

Events received on the channel are tables with these fields:

| Field | Type | Description |
|-------|------|-------------|
| system | string | System identifier of the event |
| kind | string | Event kind/type |
| path | string | Event path |
| data | any | Event payload (if provided when sent) |

```lua
local ch = sub:channel()
while true do
    local evt, ok = ch:receive()
    if not ok then break end  -- channel closed

    print(evt.system)  -- "myapp.system"
    print(evt.kind)    -- "user.created"
    print(evt.path)    -- "/users/123"
    print(evt.data)    -- {user_id=123, ...}
end
```

#### sub:close() -> boolean

Unsubscribes from the event bus and closes the channel. Receivers on the channel will get `(nil, false)`.

**Returns:** true

```lua
local sub, err = events.subscribe("myapp.*")
if err then error(err) end

-- Later, unsubscribe
sub:close()
```

## Errors

This module returns structured errors. Check kind with `errors.*` constants:

```lua
local sub, err = events.subscribe("")
if err then
    if err:kind() == errors.INVALID then
        -- invalid input (empty system)
    elseif err:kind() == errors.INTERNAL then
        -- internal error
    end
end
```

**Possible kinds:** `errors.INVALID`, `errors.INTERNAL`

## Example

```lua
local events = require("events")
local time = require("time")

-- Subscribe to all test system events
local sub, err = events.subscribe("test.*")
if err then error(err) end

local ch = sub:channel()

-- Spawn sender in background
coroutine.spawn(function()
    time.sleep(100 * time.MILLISECOND)

    local ok, err = events.send("test.orders", "order.created", "/orders/123", {
        user_id = 456,
        amount = 99.99
    })
    if err then error(err) end
end)

-- Wait for event with timeout using channel.select
local timer = time.after(2000 * time.MILLISECOND)
local result = channel.select{
    ch:case_receive(),
    timer:case_receive()
}

if result.channel == ch then
    local evt = result.value
    print("Received:", evt.system, evt.kind, evt.path)
    print("Data:", evt.data.user_id, evt.data.amount)
else
    print("Timeout")
end

-- Cleanup
sub:close()
```
