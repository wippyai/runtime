local assert = require("assert2")

local function main()
	local queue = require("queue")

	-- message function exists
	assert.not_nil(queue.message, "message function should exist")

	-- message without delivery context returns error
	local msg, err = queue.message()
	assert.is_nil(msg, "message without delivery should return nil")
	assert.not_nil(err, "message without delivery should return error")

	return true
end

return { main = main }
