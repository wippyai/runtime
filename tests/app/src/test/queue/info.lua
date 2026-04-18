-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local queue = require("queue")

local function main()
	-- info function exists
	assert.not_nil(queue.info, "info function should exist")

	-- info with empty queue ID should fail
	local stats, err = queue.info("")
	assert.is_nil(stats, "info with empty queue ID should return nil")
	assert.not_nil(err, "info with empty queue ID should return error")
	assert.eq(err:kind(), errors.INVALID, "empty queue ID error kind should be INVALID")

	-- info on unknown queue should fail with INTERNAL
	stats, err = queue.info("app.queue:nonexistent")
	assert.is_nil(stats, "info on unknown queue should return nil")
	assert.not_nil(err, "info on unknown queue should return error")
	assert.eq(err:kind(), errors.INTERNAL, "unknown queue error kind should be INTERNAL")

	-- info on real queue returns a table with driver-dependent stats
	local info, info_err = queue.info("app.queue:tasks")
	assert.is_nil(info_err, "info on real queue should not error")
	assert.not_nil(info, "info on real queue should return stats")
	assert.eq(type(info), "table", "info should be a table")
	-- memory driver populates message_count and ready
	assert.not_nil(info.message_count, "memory driver exposes message_count")
	assert.not_nil(info.ready, "memory driver exposes ready")

	return true
end

return { main = main }
