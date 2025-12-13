# Future

Asynchronous operation result container. Process, workflow.

## Overview

Future is not a loadable module. Futures are returned by `funcs.async()` and executor `async()` methods. They represent an async operation that may complete later.

## Types

### Future

Returned by `funcs.async()` and executor methods. Provides access to async operation results.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| response | () | Channel | Returns channel for receiving result |
| channel | () | Channel | Alias for response() |
| is_complete | () | boolean | Non-blocking completion check |
| is_canceled | () | boolean | Returns true if cancel() was called |
| result | () | value, error | Returns cached result or error |
| error | () | error, boolean | Returns error if failed, ok=true if has error |
| cancel | () | - | Cancels async operation, yields |

#### future:response() -> Channel

Returns the underlying channel for receiving the async result.

**Returns:** Channel (from engine, see `runtime/lua/engine/spec.md`)

First call creates the channel, subsequent calls return the same instance.

```lua
local ch = future:response()
local payload, ok = ch:receive()  -- blocks until complete
```

#### future:channel() -> Channel

Alias for `response()`. Backwards compatibility.

**Returns:** Channel

#### future:is_complete() -> boolean

Non-blocking check if the future has completed (success or error).

**Returns:** `true` if complete, `false` otherwise

```lua
if future:is_complete() then
    local val, err = future:result()
end
```

#### future:is_canceled() -> boolean

Returns whether `cancel()` was called on this future.

**Returns:** `true` if canceled, `false` otherwise

Note: Just because cancel was called doesn't mean the operation stopped. The underlying async operation may still complete.

#### future:result() -> value, error

Returns the cached result if available.

**Returns:**
- Not complete: `nil, nil`
- Canceled: `nil, error` with kind `errors.CANCELED`
- Error: `nil, error` (structured error from async operation)
- Success: `value, nil` (Payload or table of Payloads)

**Result types:**
- Single payload: `Payload` userdata (see `runtime/lua/modules/payload/spec.md`)
- Multiple payloads: table array of `Payload` userdata (indexed 1, 2, 3...)

**Non-blocking:** Does not wait for completion, returns immediately.

```lua
local val, err = future:result()
if err then
    -- handle error or not complete
elseif val then
    -- use result (Payload)
    local data = val:data()
end
```

#### future:error() -> error, boolean

Returns error if the future completed with error.

**Returns:**
- Has error: `error, true`
- No error or not complete: `nil, false`

```lua
local err, ok = future:error()
if ok then
    -- future completed with error
    print(err:kind(), err:message())
end
```

#### future:cancel() -> -

Cancels the async operation. Marks the future as canceled.

**Yields:** until cancel command is sent

**Note:** Cancel is best-effort. The async operation may still complete if already in progress.

```lua
local future = funcs.async("app.test:slow", 5000)
future:cancel()  -- attempt to cancel

-- Later:
if future:is_canceled() then
    print("was canceled")
end
```

## Dependencies

### Channel (from engine)

Used by `response()` and `channel()` for receiving results.

| Method | Signature | Returns |
|--------|-----------|---------|
| receive | () | value, ok: boolean |
| close | () | - |
| case_receive | () | case |

See: `runtime/lua/engine/spec.md`

### Payload (from payload module)

Results are returned as Payload userdata.

| Method | Signature | Returns |
|--------|-----------|---------|
| data | () | value, error |
| get_format | () | string |

See: `runtime/lua/modules/payload/spec.md`

## Errors

Future methods return structured errors. Check kind with `errors.*` constants:

```lua
local val, err = future:result()
if err then
    if err:kind() == errors.CANCELED then
        -- operation was canceled
    elseif err:kind() == errors.INTERNAL then
        -- async operation failed
    end
end
```

**Possible kinds:** `errors.CANCELED`, `errors.INTERNAL`, or any error kind from the async operation

## Example

```lua
local funcs = require("funcs")

-- Start async operation
local future, err = funcs.async("app.process:heavy_compute", 42)
if err then error(err) end

-- Non-blocking check
if not future:is_complete() then
    print("still running...")
end

-- Wait for result via channel
local ch = future:response()
local payload, ok = ch:receive()
if not ok then
    error("channel closed unexpectedly")
end

local data = payload:data()
print("result:", data)

-- Alternative: use result() method
local val2, err2 = future:result()
if err2 then
    error(err2)
end
-- val2 is same as payload (cached)

-- Using multiple futures with select
local f1 = funcs.async("app.process:task_a")
local f2 = funcs.async("app.process:task_b")

local result = channel.select{
    f1:channel():case_receive(),
    f2:channel():case_receive()
}

if result.channel == f1:channel() then
    print("task_a completed first")
else
    print("task_b completed first")
end
```
