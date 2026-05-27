-- SPDX-License-Identifier: MPL-2.0

-- The test consumer is declared with concurrency=2 in
-- tests/app/src/queue/_index.yaml. Each handler writes a per-correlation
-- "alive" sentinel and spins waiting for the peer's sentinel to appear.
-- Per-handler keys sidestep the read-modify-write race a shared counter
-- has under real parallelism. If the consumer dispatches handlers
-- serially, handler A posts, spins to its timeout without ever seeing B,
-- records overlapped=false, and the assertion fires with the message
-- below — exactly the signal a regression would produce.

local assert = require("assert2")
local queue = require("queue")
local store = require("store")
local time = require("time")

local function main()
	local s, store_err = store.get("app.test.store:memory")
	assert.is_nil(store_err, "should get store")

	local batch = "conc-batch-" .. tostring(time.now():unix_nano())
	local corr_a = batch .. "-a"
	local corr_b = batch .. "-b"

	-- Pre-clear sentinels in case of state from a previous run.
	s:delete("queue:concurrency:alive:" .. batch .. ":" .. corr_a)
	s:delete("queue:concurrency:alive:" .. batch .. ":" .. corr_b)

	local _, err_a = queue.publish("app.queue:tasks",
		{ action = "concurrency_probe", batch = batch, peer = corr_b },
		{ correlation_id = corr_a })
	assert.is_nil(err_a, "publish A should succeed")

	local _, err_b = queue.publish("app.queue:tasks",
		{ action = "concurrency_probe", batch = batch, peer = corr_a },
		{ correlation_id = corr_b })
	assert.is_nil(err_b, "publish B should succeed")

	-- Spin budget inside each handler is ~4s (200 × 20ms); the outer
	-- window must exceed that for the serial-consumer failure mode to
	-- surface cleanly instead of masking as an incomplete test.
	local rec_a, rec_b
	for _ = 1, 200 do
		time.sleep("50ms")
		rec_a = rec_a or s:get("queue:concurrency:" .. corr_a)
		rec_b = rec_b or s:get("queue:concurrency:" .. corr_b)
		if rec_a and rec_b then break end
	end

	assert.not_nil(rec_a, "handler should have processed A")
	assert.not_nil(rec_b, "handler should have processed B")
	assert.neq(rec_a.msg_id, rec_b.msg_id, "distinct message ids")

	assert.eq(rec_a.overlapped, true,
		"handler A must have observed handler B's sentinel — serial consumer would miss it")
	assert.eq(rec_b.overlapped, true,
		"handler B must have observed handler A's sentinel")

	return true
end

return { main = main }
