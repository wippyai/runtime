# Engine Primitives

Core engine components globally available to all Lua code. Process, concurrency.

## channel

Go-style channels for inter-coroutine communication.

### channel.new(bufSize?: integer) -> channel

Creates a new channel.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| bufSize | integer | no | 0 | Buffer capacity. 0 = unbuffered (synchronous) |

**Returns:** channel userdata

```lua
local unbuffered = channel.new()      -- unbuffered, blocks until receiver ready
local buffered = channel.new(10)      -- buffered, can hold 10 values before blocking
```

### channel.select(cases: table) -> table

Waits on multiple channel operations, executes the first one ready.

| Param | Type | Required | Notes |
|-------|------|----------|-------|
| cases | table | yes | Array of case objects, optionally with `default = true` |

**cases table:**
- Array part: case objects from `ch:case_send(value)` or `ch:case_receive()`
- `default = true`: (optional) makes select non-blocking, returns immediately if no case ready

**Returns:** table with fields:
- `channel` - the channel that was ready (nil if default taken)
- `value` - received value (for receive cases) or sent value (for send cases)
- `ok` - true if operation succeeded, false if channel closed
- `default` - true if default case was taken

```lua
local ch1 = channel.new(1)
local ch2 = channel.new(1)

ch1:send("hello")

-- Blocking select
local result = channel.select{
    ch1:case_receive(),
    ch2:case_receive()
}
-- result.channel == ch1
-- result.value == "hello"
-- result.ok == true

-- Non-blocking select with default
local result2 = channel.select{
    ch1:case_receive(),
    ch2:case_receive(),
    default = true   -- key inside the table, not second argument
}
-- If no channel ready: result2.default == true, result2.ok == true
-- If channel ready: result2.default == nil, normal result
```

## channel (instance methods)

Methods available on channel userdata.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| send | (value: any) | true | Blocks until receiver ready (unbuffered) or buffer available |
| receive | () | value, ok: boolean | Blocks until value available. ok=false when closed |
| close | () | - | Wakes blocked receivers with (nil, false). Error to send after close |
| case_send | (value: any) | case | Creates send case for select |
| case_receive | () | case | Creates receive case for select |

### ch:send(value: any) -> true

Sends a value to the channel. Blocks if channel is full (unbuffered or buffer at capacity).

**Returns:** true on success

**Errors:** raises error if channel is closed

```lua
local ch = channel.new(1)
ch:send("hello")    -- returns true
ch:send("world")    -- blocks until receiver
```

### ch:receive() -> value, ok

Receives a value from the channel. Blocks if channel is empty.

**Returns:**
- Success: `value, true` - received value and ok=true
- Closed empty channel: `nil, false`

```lua
local ch = channel.new(1)
ch:send("hello")
local val, ok = ch:receive()  -- val="hello", ok=true

ch:close()
val, ok = ch:receive()        -- val=nil, ok=false
```

### ch:close()

Closes the channel. Blocked senders receive error. Blocked receivers receive (nil, false).
Buffered values can still be received after close.

```lua
local ch = channel.new(1)
ch:send("buffered")
ch:close()

local v1, ok1 = ch:receive()  -- v1="buffered", ok1=true
local v2, ok2 = ch:receive()  -- v2=nil, ok2=false
```

### ch:case_send(value: any) -> case

Creates a send case for use with channel.select.

```lua
local case = ch:case_send("hello")
local result = channel.select{case}
```

### ch:case_receive() -> case

Creates a receive case for use with channel.select.

```lua
local case = ch:case_receive()
local result = channel.select{case}
-- result.value contains received value
```

## coroutine.spawn

Spawns a managed coroutine that runs concurrently, similar to a goroutine.

### coroutine.spawn(fn: function) -> thread

| Param | Type | Required | Notes |
|-------|------|----------|-------|
| fn | function | yes | Function to execute concurrently |

**Returns:** thread (the new coroutine)

**Key differences from standard coroutines:**
- **Managed:** No need to manually resume - scheduler handles execution
- **Concurrent:** Runs in parallel with caller, not cooperatively
- **Memory-safe:** Runtime guarantees no data races at memory level
- **Just call:** Pass a function, it runs - no yield/resume dance

```lua
local ch = channel.new(0)

coroutine.spawn(function()
    -- This runs concurrently, not cooperatively
    local val = ch:receive()  -- blocks until sender ready
    print("received:", val)
end)

ch:send("hello")  -- unblocks the spawned coroutine
```

## coroutine (standard library)

Standard Lua 5.3 coroutine functions remain available (`coroutine.create`, `coroutine.resume`, `coroutine.yield`, etc.) for manual cooperative multitasking. Use `coroutine.spawn` for managed concurrent execution.

## Example

```lua
-- Producer-consumer pattern
local ch = channel.new(5)
local done = channel.new(1)

-- Producer
coroutine.spawn(function()
    for i = 1, 10 do
        ch:send(i)
    end
    ch:close()
end)

-- Consumer
coroutine.spawn(function()
    local sum = 0
    while true do
        local v, ok = ch:receive()
        if not ok then break end
        sum = sum + v
    end
    done:send(sum)
end)

local total = done:receive()  -- 55

-- Select with multiple channels
local fast = channel.new(1)
local slow = channel.new(1)

coroutine.spawn(function()
    -- simulate work
    fast:send("fast result")
end)

coroutine.spawn(function()
    -- simulate slower work
    slow:send("slow result")
end)

local result = channel.select{
    fast:case_receive(),
    slow:case_receive()
}
-- result.value is whichever arrived first
```
