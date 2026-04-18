-- SPDX-License-Identifier: MPL-2.0

-- Live-wire single-shot ack/nack coordination. On SQS, ack = DeleteMessage
-- and nack = ChangeMessageVisibility. The handler tries both a double-ack
-- and a follow-up nack; all three must resolve locally (no second broker
-- round-trip) and the second/third calls must return structured INVALID
-- errors. Mirrors tests/app/src/test/queue/settle_coordination.lua but
-- against a real SQS broker.

local assert = require("assert2")
local queue = require("queue")
local store = require("store")
local time = require("time")

local function main()
	local s, store_err = store.get("app.test.store:memory")
	assert.is_nil(store_err, "should get store")

	local corr = "sqs-settle-" .. tostring(time.now():unix_nano())

	local ok, err = queue.publish("app.test.sqs:tasks",
		{ action = "double_settle_probe", correlation = corr },
		{ correlation_id = corr })
	assert.is_nil(err, "publish should succeed")
	assert.eq(ok, true, "publish should return true")

	local rec
	for _ = 1, 60 do
		rec = s:get("sqs:settle_probe:" .. corr)
		if rec then break end
		time.sleep("200ms")
	end

	assert.not_nil(rec, "handler should have recorded the settle probe outcome")

	assert.eq(rec.first_ok, true, "first ack must succeed on the broker")
	assert.eq(rec.first_err_nil, true, "first ack must not return an error")

	assert.is_nil(rec.second_ok, "second ack must not claim success")
	assert.eq(rec.second_err_kind, errors.INVALID,
		"second ack must surface a structured INVALID error")

	assert.is_nil(rec.nack_ok, "post-ack nack must not claim success")
	assert.eq(rec.nack_err_kind, errors.INVALID,
		"nack after ack must surface a structured INVALID error")

	return true
end

return { main = main }
