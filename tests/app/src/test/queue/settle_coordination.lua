-- SPDX-License-Identifier: MPL-2.0

-- queue.Message ack/nack are single-shot. The first call settles the
-- delivery; every subsequent ack/nack on the same wrapper must surface
-- a structured INVALID error ("queue.Message already settled") instead
-- of emitting another broker frame. This test exercises that contract
-- end-to-end through the task_handler.

local assert = require("assert2")
local queue = require("queue")
local store = require("store")
local time = require("time")

local function main()
	local s, store_err = store.get("app.test.store:memory")
	assert.is_nil(store_err, "should get store")

	local corr = "settle-coord-" .. tostring(time.now():unix_nano())

	local ok, err = queue.publish("app.queue:tasks", {
		action = "double_settle_probe",
	}, {
		correlation_id = corr,
	})
	assert.is_nil(err, "publish should succeed")
	assert.eq(ok, true, "publish should return true")

	-- Poll for the handler to record its outcome.
	local outcome
	for _ = 1, 50 do
		time.sleep("50ms")
		outcome = s:get("queue:settle_probe:" .. corr)
		if outcome then break end
	end

	assert.not_nil(outcome, "handler should have recorded settle-probe outcome")

	-- First ack wins.
	assert.eq(outcome.first_ok, true, "first ack should succeed")
	assert.eq(outcome.first_err_nil, true, "first ack should return no error")

	-- Second ack refused with structured INVALID.
	assert.is_nil(outcome.second_ok, "second ack should return nil on refusal")
	assert.eq(outcome.second_err_kind, errors.INVALID, "second ack must surface INVALID kind")
	assert.not_nil(outcome.second_err_msg, "second ack error should carry a message")

	-- Nack after ack also refused — same settle slot.
	assert.is_nil(outcome.nack_ok, "post-settle nack should return nil on refusal")
	assert.eq(outcome.nack_err_kind, errors.INVALID, "post-settle nack must surface INVALID kind")

	return true
end

return { main = main }
