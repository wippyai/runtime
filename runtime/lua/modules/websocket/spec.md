<!-- SPDX-License-Identifier: MPL-2.0 -->

# websocket

WebSocket client connections. Network, io, nondeterministic.

## Loading

```lua
local websocket = require("websocket")
```

## Constants

```lua
websocket.TYPE_TEXT                    -- "text"
websocket.TYPE_BINARY                  -- "binary"
websocket.TYPE_PING                    -- "ping"
websocket.TYPE_PONG                    -- "pong"
websocket.TYPE_CLOSE                   -- "close"
websocket.TEXT                         -- 1
websocket.BINARY                       -- 2
websocket.COMPRESSION.DISABLED         -- 0
websocket.COMPRESSION.CONTEXT_TAKEOVER -- 1
websocket.COMPRESSION.NO_CONTEXT       -- 2
websocket.CLOSE_CODES.NORMAL           -- 1000
websocket.CLOSE_CODES.GOING_AWAY       -- 1001
websocket.CLOSE_CODES.PROTOCOL_ERROR   -- 1002
websocket.CLOSE_CODES.UNSUPPORTED_DATA -- 1003
websocket.CLOSE_CODES.RESERVED         -- 1004
websocket.CLOSE_CODES.NO_STATUS        -- 1005
websocket.CLOSE_CODES.ABNORMAL_CLOSURE -- 1006
websocket.CLOSE_CODES.INVALID_PAYLOAD  -- 1007
websocket.CLOSE_CODES.POLICY_VIOLATION -- 1008
websocket.CLOSE_CODES.MESSAGE_TOO_BIG  -- 1009
websocket.CLOSE_CODES.MANDATORY_EXTENSION -- 1010
websocket.CLOSE_CODES.INTERNAL_ERROR   -- 1011
websocket.CLOSE_CODES.SERVICE_RESTART  -- 1012
websocket.CLOSE_CODES.TRY_AGAIN_LATER  -- 1013
websocket.CLOSE_CODES.BAD_GATEWAY      -- 1014
websocket.CLOSE_CODES.TLS_HANDSHAKE    -- 1015
```

## Dependencies

### channel (from engine)

Used by `client:channel()` for receiving messages.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| receive | () | value, ok: boolean | Blocks until value available. ok=false when closed |
| close | () | - | Closes channel |

See: `runtime/lua/engine/spec.md`

## Functions

### connect(url: string, options?: table) -> Client, error

Establishes a WebSocket connection.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| url | string | yes | - | WebSocket URL (ws:// or wss://) |
| options | table | no | nil | Connection options |

**options fields:**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| headers | {[string]: string} | nil | HTTP headers for handshake |
| protocols | string[] | nil | WebSocket subprotocols |
| dial_timeout | integer\|string | 0 | Milliseconds or Go duration ("5s") |
| read_timeout | integer\|string | 0 | Milliseconds or Go duration ("5s") |
| write_timeout | integer\|string | 0 | Milliseconds or Go duration ("5s") |
| compression | integer\|string | 0 | Mode: 0/"disabled", 1/"context_takeover", 2/"no_context_takeover" |
| compression_threshold | integer | 0 | Minimum message size for compression (0-104857600 bytes) |
| read_limit | integer | 0 | Maximum message size in bytes (0-134217728) |
| channel_capacity | integer | 32 | Channel buffer size (1-10000) |

**Returns:**
- Success: `Client, nil` - Client userdata
- Error: `nil, error` - error is string

**Errors (strings):**
- `"no context"` - missing context
- `"websocket connections not allowed"` - security policy
- `"not allowed: {url}"` - URL not allowed by policy
- Connection errors from underlying transport

**Yields:** until connection established or timeout

```lua
local client, err = websocket.connect("wss://echo.example.com", {
    headers = { Authorization = "Bearer token" },
    dial_timeout = 5000,
    compression = "context_takeover"
})
```

## Types

### Client

Returned by `websocket.connect()`.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| send | (data: string, type?: integer) | - | Yields until sent. type: websocket.TEXT (1) or websocket.BINARY (2) |
| channel | () | channel | First call yields to subscribe, subsequent calls return same channel |
| receive | () | channel | Alias for channel() |
| ping | () | - | Yields until ping sent |
| close | (code?: integer, reason?: string) | - | Yields until close sent. code default 1000 |

#### client:send(data: string, type?: integer)

Sends a message to the server.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| data | string | yes | - | Message data |
| type | integer | no | 1 | websocket.TEXT (1) or websocket.BINARY (2) |

**Yields:** until message sent

```lua
client:send("Hello")
client:send("\x00\x01\x02", websocket.BINARY)
```

#### client:channel() -> channel

Returns channel for receiving messages. First call yields to subscribe and returns channel. Subsequent calls return the same channel without yielding.

**Returns:** channel userdata

**Yields:** on first call only, until subscription established

**Channel messages:**

Messages received on the channel are tables with:

| Field | Type | Notes |
|-------|------|-------|
| type | string | "text" or "binary" |
| data | string | Message data |

```lua
local ch = client:channel()  -- first call yields
while true do
    local msg, ok = ch:receive()
    if not ok then break end   -- channel closed
    print(msg.type, msg.data)
end
```

#### client:receive() -> channel

Alias for `client:channel()`. Provided for v1 API compatibility.

#### client:ping()

Sends a ping to the server.

**Yields:** until ping sent

```lua
client:ping()
```

#### client:close(code?: integer, reason?: string)

Closes the WebSocket connection.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| code | integer | no | 1000 | Close code (1000-4999) |
| reason | string | no | "" | Close reason |

**Yields:** until close sent

```lua
client:close(websocket.CLOSE_CODES.NORMAL, "done")
```

## Example

```lua
local websocket = require("websocket")

local client, err = websocket.connect("wss://echo.websocket.org", {
    dial_timeout = 5000
})
if err then error(err) end

client:send("Hello WebSocket!")

local ch = client:channel()
local msg, ok = ch:receive()
if ok and msg.type == "text" then
    print("Received:", msg.data)
end

client:close(websocket.CLOSE_CODES.NORMAL)
```
