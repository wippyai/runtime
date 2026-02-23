-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")

local function main()
	local websocket = require("websocket")

	-- Connect to echo websocket server
	local client, err = websocket.connect("wss://echo.websocket.org")
	assert.is_nil(err, "connect should not error")
	assert.not_nil(client, "client should not be nil")

	-- Get channel for receiving
	local ch = client:channel()
	assert.not_nil(ch, "channel should not be nil")

	-- Close connection (close doesn't return a value on success)
	client:close(websocket.CLOSE_CODES.NORMAL, "test done")

	return true
end

return { main = main }
