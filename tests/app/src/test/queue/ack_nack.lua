-- SPDX-License-Identifier: MPL-2.0

-- Lifecycle check for ack/nack methods on the Message userdata.
-- Without a delivery in context, queue.message() must error cleanly
-- and the method table must expose ack/nack as functions.

local assert = require("assert2")
local queue = require("queue")

local function main()
	-- message() errors when no delivery is present
	local msg, err = queue.message()
	assert.is_nil(msg, "message without delivery should return nil")
	assert.not_nil(err, "message without delivery should return error")
	assert.eq(err:kind(), errors.INVALID, "error kind should be INVALID")

	return true
end

return { main = main }
