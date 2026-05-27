-- SPDX-License-Identifier: MPL-2.0

-- Neutral + sqs-prefixed headers must round-trip through SQS
-- MessageAttributes back to the consumer. This is the live-wire
-- equivalent of the memory-driver headers test.

local assert = require("assert2")
local queue = require("queue")
local store = require("store")
local time = require("time")

local function main()
	local s, store_err = store.get("app.test.store:memory")
	assert.is_nil(store_err, "should get store")

	local corr = "sqs-headers-" .. tostring(time.now():unix_nano())

	local ok, err = queue.publish("app.test.sqs:tasks",
		{ action = "headers_probe", correlation = corr },
		{
			correlation_id = corr,
			-- sqs.message_attributes.* must land on the consumer as a
			-- verbatim header — drivers copy through any key they
			-- don't special-case.
			["sqs.message_attributes.tenant"] = "acme",
		})
	assert.is_nil(err, "publish should succeed")
	assert.eq(ok, true, "publish should return true")

	local rec
	for _ = 1, 60 do
		rec = s:get("sqs:by_corr:" .. corr)
		if rec then break end
		time.sleep("200ms")
	end

	assert.not_nil(rec, "consumer should have processed the headers message")
	assert.eq(rec.headers.correlation_id, corr,
		"correlation_id must be visible on the consumer side")

	-- The tenant attribute was set via the sqs.message_attributes.* prefix
	-- and must survive the SendMessage -> ReceiveMessage round-trip. The
	-- key it arrives under depends on the driver's translation; accept
	-- either the prefixed form or the bare key, but require the value.
	local tenant = rec.headers["sqs.message_attributes.tenant"]
		or rec.headers["tenant"]
	assert.eq(tenant, "acme", "tenant header must round-trip through SQS")

	return true
end

return { main = main }
