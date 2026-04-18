-- SPDX-License-Identifier: MPL-2.0

-- queue.info against a live SQS endpoint. The driver queries SQS queue
-- attributes and surfaces a minimal canonical set; assert presence rather
-- than exact values since another test's in-flight traffic can move them.

local assert = require("assert2")
local queue = require("queue")

local function main()
	local stats, err = queue.info("app.test.sqs:tasks")
	assert.is_nil(err, "info should succeed against declared SQS queue")
	assert.not_nil(stats, "info should return a stats table")

	-- Presence check: message_count is driver-provided and must exist.
	-- Value may be any non-negative integer; exact count depends on
	-- whatever messages the earlier tests left unconsumed.
	assert.not_nil(stats.message_count,
		"SQS driver must report message_count in info stats")

	return true
end

return { main = main }
