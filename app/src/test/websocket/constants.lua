local assert = require("assert2")

local function main()
    local websocket = require("websocket")

    -- Message types (strings)
    assert.eq(websocket.TYPE_TEXT, "text", "TYPE_TEXT should be 'text'")
    assert.eq(websocket.TYPE_BINARY, "binary", "TYPE_BINARY should be 'binary'")
    assert.eq(websocket.TYPE_PING, "ping", "TYPE_PING should be 'ping'")
    assert.eq(websocket.TYPE_PONG, "pong", "TYPE_PONG should be 'pong'")
    assert.eq(websocket.TYPE_CLOSE, "close", "TYPE_CLOSE should be 'close'")

    -- Numeric constants
    assert.eq(websocket.TEXT, 1, "TEXT should be 1")
    assert.eq(websocket.BINARY, 2, "BINARY should be 2")

    -- Compression modes
    assert.eq(websocket.COMPRESSION.DISABLED, 0, "COMPRESSION.DISABLED should be 0")
    assert.eq(websocket.COMPRESSION.CONTEXT_TAKEOVER, 1, "COMPRESSION.CONTEXT_TAKEOVER should be 1")
    assert.eq(websocket.COMPRESSION.NO_CONTEXT, 2, "COMPRESSION.NO_CONTEXT should be 2")

    -- Close codes
    assert.eq(websocket.CLOSE_CODES.NORMAL, 1000, "CLOSE_CODES.NORMAL should be 1000")
    assert.eq(websocket.CLOSE_CODES.GOING_AWAY, 1001, "CLOSE_CODES.GOING_AWAY should be 1001")
    assert.eq(websocket.CLOSE_CODES.PROTOCOL_ERROR, 1002, "CLOSE_CODES.PROTOCOL_ERROR should be 1002")
    assert.eq(websocket.CLOSE_CODES.UNSUPPORTED_DATA, 1003, "CLOSE_CODES.UNSUPPORTED_DATA should be 1003")
    assert.eq(websocket.CLOSE_CODES.ABNORMAL_CLOSURE, 1006, "CLOSE_CODES.ABNORMAL_CLOSURE should be 1006")
    assert.eq(websocket.CLOSE_CODES.INTERNAL_ERROR, 1011, "CLOSE_CODES.INTERNAL_ERROR should be 1011")
    assert.eq(websocket.CLOSE_CODES.TLS_HANDSHAKE, 1015, "CLOSE_CODES.TLS_HANDSHAKE should be 1015")

    -- Connect function exists
    assert.not_nil(websocket.connect, "connect function should exist")

    return true
end

return { main = main }
