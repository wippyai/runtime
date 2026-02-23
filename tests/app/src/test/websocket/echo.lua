-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")

local function main()
	local websocket = require("websocket")

	-- Connect to websocket echo server
	local client, err = websocket.connect("wss://ws.postman-echo.com/raw")
	assert.is_nil(err, "connect should not error")
	assert.not_nil(client, "client should not be nil")

	-- Send a message
	client:send("Hello WebSocket!")

	-- Get channel and receive the echo
	local ch = client:channel()
	local msg, ok = ch:receive()

	assert.eq(ok, true, "receive should succeed")
	assert.not_nil(msg, "message should not be nil")
	assert.eq(msg.type, "text", "message type should be text")
	assert.eq(msg.data, "Hello WebSocket!", "message should echo back")

	-- Send another message
	client:send("Second message")
	msg, ok = ch:receive()
	assert.eq(ok, true, "second receive should succeed")
	assert.eq(msg.data, "Second message", "second message should echo back")

	-- Close connection
	client:close(websocket.CLOSE_CODES.NORMAL, "test complete")

	return true
end

return { main = main }
