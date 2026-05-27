-- SPDX-License-Identifier: MPL-2.0

-- End-to-end: publish a message, wait for the consumer to record the
-- handler's observed body/headers in the store, assert the payload is
-- intact. Proves the full Lua publish -> SQS wire -> consumer delivery
-- -> Lua handler -> payload decode chain works against a real broker.

local assert = require("assert2")
local queue = require("queue")
local store = require("store")
local time = require("time")

local function main()
	local s, store_err = store.get("app.test.store:memory")
	assert.is_nil(store_err, "should get store")

	local corr = "sqs-roundtrip-" .. tostring(time.now():unix_nano())

	local ok, err = queue.publish("app.test.sqs:tasks",
		{ action = "roundtrip", correlation = corr, value = 42 },
		{ correlation_id = corr })
	assert.is_nil(err, "publish should succeed")
	assert.eq(ok, true, "publish should return true")

	-- SQS long-poll + consumer loop: allow generous time for the
	-- message to come back through ReceiveMessage.
	local rec
	for _ = 1, 60 do
		rec = s:get("sqs:by_corr:" .. corr)
		if rec then break end
		time.sleep("200ms")
	end

	assert.not_nil(rec, "consumer should have processed the roundtrip message within ~12s")
	assert.eq(rec.correlation_id, corr, "correlation_id should round-trip")
	assert.eq(rec.body.action, "roundtrip", "body.action should round-trip")
	assert.eq(rec.body.value, 42, "numeric body field should round-trip")

	return true
end

return { main = main }
