# Lua WebSocket Module Specification

## Overview

The `websocket` module provides a Lua interface for WebSocket client connections. It supports connecting to WebSocket servers, sending/receiving messages, and handling connection lifecycle.

## Module Interface

### Module Loading

```lua
local websocket = require("websocket")
```

### Functions

#### websocket.connect(url: string, options?: table)

Establishes a WebSocket connection.

Parameters:
- `url`: WebSocket URL (ws:// or wss://)
- `options`: (Optional) Connection options table:
  - `headers`: Table of HTTP headers to send during handshake
  - `dial_timeout`: Connection timeout in seconds
  - `compression`: Compression mode ("disabled", "context_takeover", "no_context_takeover")
  - `compression_threshold`: Minimum message size for compression
  - `read_limit`: Maximum message size in bytes

Returns:
- `client, error`: WebSocket client object on success, or `nil` and error message on failure

```lua
local client, err = websocket.connect("wss://echo.websocket.org", {
    headers = { Authorization = "Bearer token" },
    dial_timeout = 5,
    compression = "context_takeover"
})
```

## Client Methods

### client:send(data: string, messageType?: number)

Sends a message to the server.

Parameters:
- `data`: Message data as string
- `messageType`: (Optional) Message type (websocket.TEXT or websocket.BINARY, defaults to TEXT)

```lua
client:send("Hello, WebSocket!")
client:send("\x00\x01\x02", websocket.BINARY)
```

### client:channel()

Returns a channel object for receiving messages.

Returns:
- `channel`: WebSocket channel object

```lua
local ch = client:channel()
```

### client:receive()

Alias for `client:channel()` for v1 API compatibility.

### client:ping()

Sends a ping to the server.

```lua
client:ping()
```

### client:close(code?: number, reason?: string)

Closes the connection.

Parameters:
- `code`: (Optional) Close status code (default: 1000)
- `reason`: (Optional) Close reason string

```lua
client:close(websocket.CLOSE_CODES.NORMAL, "done")
```

## Channel Methods

### channel:receive()

Waits for and returns the next message.

Returns:
- `message, ok`: Message table and boolean success flag

Message table fields:
- `type`: Message type ("text", "binary", or "close")
- `data`: Message data (string)

```lua
local ch = client:channel()
local msg, ok = ch:receive()
if ok and msg.type == "text" then
    print("Received:", msg.data)
end
```

## Constants

### Message Types (Strings)

```lua
websocket.TYPE_TEXT    -- "text"
websocket.TYPE_BINARY  -- "binary"
websocket.TYPE_PING    -- "ping"
websocket.TYPE_PONG    -- "pong"
websocket.TYPE_CLOSE   -- "close"
```

### Message Types (Numeric)

```lua
websocket.TEXT    -- 1
websocket.BINARY  -- 2
```

### Compression Modes

```lua
websocket.COMPRESSION.DISABLED         -- 0
websocket.COMPRESSION.CONTEXT_TAKEOVER -- 1
websocket.COMPRESSION.NO_CONTEXT       -- 2
```

### Close Codes

```lua
websocket.CLOSE_CODES.NORMAL              -- 1000
websocket.CLOSE_CODES.GOING_AWAY          -- 1001
websocket.CLOSE_CODES.PROTOCOL_ERROR      -- 1002
websocket.CLOSE_CODES.UNSUPPORTED_DATA    -- 1003
websocket.CLOSE_CODES.RESERVED            -- 1004
websocket.CLOSE_CODES.NO_STATUS           -- 1005
websocket.CLOSE_CODES.ABNORMAL_CLOSURE    -- 1006
websocket.CLOSE_CODES.INVALID_PAYLOAD     -- 1007
websocket.CLOSE_CODES.POLICY_VIOLATION    -- 1008
websocket.CLOSE_CODES.MESSAGE_TOO_BIG     -- 1009
websocket.CLOSE_CODES.MANDATORY_EXTENSION -- 1010
websocket.CLOSE_CODES.INTERNAL_ERROR      -- 1011
websocket.CLOSE_CODES.SERVICE_RESTART     -- 1012
websocket.CLOSE_CODES.TRY_AGAIN_LATER     -- 1013
websocket.CLOSE_CODES.BAD_GATEWAY         -- 1014
websocket.CLOSE_CODES.TLS_HANDSHAKE       -- 1015
```

## Example Usage

```lua
local websocket = require("websocket")

-- Connect to echo server
local client, err = websocket.connect("wss://echo.websocket.org")
if err then
    error("connect failed: " .. err)
end

-- Send a message
client:send("Hello, WebSocket!")

-- Receive the echo
local ch = client:channel()
local msg, ok = ch:receive()
if ok and msg.type == "text" then
    print("Received:", msg.data)
end

-- Close connection
client:close(websocket.CLOSE_CODES.NORMAL, "done")
```

## Module Classification

- **Class**: `network`, `io`, `nondeterministic`

## Go Implementation

```go
var Module = &luaapi.ModuleDef{
    Name:        "websocket",
    Description: "WebSocket client connections",
    Class:       []string{luaapi.ClassNetwork, luaapi.ClassIO, luaapi.ClassNondeterministic},
    Build:       buildModule,
}
```
