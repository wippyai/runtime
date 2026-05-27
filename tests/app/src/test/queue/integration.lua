-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local queue = require("queue")
local store = require("store")
local time = require("time")

local function main()
-- Get store instance (same one used by task_handler)
	local s, store_err = store.get("app.test.store:memory")
	assert.is_nil(store_err, "should get store")

	-- Clear counter before test
	s:delete("queue:counter")

	local corr_id = "integration-" .. tostring(time.now():unix_nano())

	-- Publish a message with neutral headers only. Driver-specific knobs
	-- (amqp.mandatory, sqs.*) are verified separately in header_overrides.lua.
	local ok, err = queue.publish("app.queue:tasks", {
		action = "integration_test",
		timestamp = tostring(time.now():unix_nano())
	}, {
		correlation_id = corr_id,
		priority = 5,
		partition_key = "tenant-42",
	})

	assert.is_nil(err, "publish should not return error")
	assert.eq(ok, true, "publish should return true")

	-- Poll for consumer to process message
	local processed = false
	for _ = 1, 50 do
		time.sleep("50ms")
		local counter, _ = s:get("queue:counter")
		if counter and counter > 0 then
			processed = true
			break
		end
	end

	assert.eq(processed, true, "message should be processed by consumer")

	-- Look up the processed record by correlation_id.
	local rec, rec_err = s:get("queue:by_corr:" .. corr_id)
	assert.is_nil(rec_err, "should read by correlation_id")
	assert.not_nil(rec, "handler should have stored record keyed by correlation_id")
	assert.eq(rec.correlation_id, corr_id, "record should carry the correlation_id we sent")
	assert.not_nil(rec.headers, "record should carry the headers table")
	assert.eq(rec.headers.priority, 5, "priority header should round-trip")
	assert.eq(rec.headers.partition_key, "tenant-42", "partition_key should round-trip")

	return true
end

return { main = main }
