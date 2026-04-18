-- SPDX-License-Identifier: MPL-2.0

-- Verifies that per-publish headers reach the consumer verbatim and
-- that both neutral keys (priority, correlation_id, partition_key) and
-- driver-prefixed keys (amqp.*, sqs.*) survive the round-trip.

local assert = require("assert2")
local queue = require("queue")
local store = require("store")
local time = require("time")

local function main()
	local s, store_err = store.get("app.test.store:memory")
	assert.is_nil(store_err, "should get store")

	local corr = "hdr-override-" .. tostring(time.now():unix_nano())

	local ok, err = queue.publish("app.queue:tasks", { action = "override_probe" }, {
		correlation_id = corr,
		priority = 9,
		partition_key = "customer-7",
		reply_to = "app.queue:replies",
		["amqp.expiration"] = "30000",
		["sqs.message_attributes.tenant"] = "acme",
	})
	assert.is_nil(err, "publish should not return error")
	assert.eq(ok, true, "publish should succeed")

	-- Poll for the handler to store the record.
	local rec
	for _ = 1, 50 do
		time.sleep("50ms")
		rec = s:get("queue:by_corr:" .. corr)
		if rec then break end
	end

	assert.not_nil(rec, "consumer should have processed the message")
	assert.eq(rec.correlation_id, corr, "correlation_id preserved")
	assert.eq(rec.headers.priority, 9, "priority preserved")
	assert.eq(rec.headers.partition_key, "customer-7", "partition_key preserved")
	assert.eq(rec.headers.reply_to, "app.queue:replies", "reply_to preserved")
	assert.eq(rec.headers["amqp.expiration"], "30000", "amqp-prefixed header passed through")
	assert.eq(rec.headers["sqs.message_attributes.tenant"], "acme", "sqs-prefixed header passed through")

	return true
end

return { main = main }
