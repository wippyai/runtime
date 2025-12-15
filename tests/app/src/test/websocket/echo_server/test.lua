-- Test: WebSocket echo integration with local wsrelay server
local assert = require("assert2")

local function main()
    local websocket = require("websocket")
    local time = require("time")
    local channel = require("channel")
    local json = require("json")

    -- Connect to local WebSocket echo server via wsrelay with timeout
    local client, err = websocket.connect("ws://localhost:8085/ws/echo", {
        dial_timeout = 3
    })

    if err then
        return false, "connect error: " .. tostring(err)
    end
    assert.not_nil(client, "client should not be nil")

    -- Send a test message
    client:send("Hello Echo Server!")

    -- Get channel and receive with timeout
    local ch = client:channel()
    local timeout = time.after("5s")

    local result = channel.select {
        ch:case_receive(),
        timeout:case_receive()
    }

    assert.not_nil(result, "select should return result")

    if result.channel == timeout then
        return false, "timeout waiting for echo response"
    end

    assert.eq(result.ok, true, "receive should succeed")

    local msg = result.value
    assert.not_nil(msg, "message should not be nil")
    assert.eq(msg.type, "text", "message type should be text")

    -- The wsrelay wraps messages in {topic, data} format
    local decoded = json.decode(msg.data)
    assert.not_nil(decoded, "message should be valid JSON")
    assert.eq(decoded.data, "Hello Echo Server!", "should echo back the message")

    -- Send another message to verify continued operation
    client:send("Second message")

    timeout = time.after("5s")
    result = channel.select {
        ch:case_receive(),
        timeout:case_receive()
    }

    if result.channel == timeout then
        return false, "timeout waiting for second echo response"
    end

    assert.eq(result.ok, true, "second receive should succeed")

    decoded = json.decode(result.value.data)
    assert.eq(decoded.data, "Second message", "second message should echo back")

    -- Close connection gracefully
    client:close(websocket.CLOSE_CODES.NORMAL, "test complete")

    return true
end

return { main = main }
