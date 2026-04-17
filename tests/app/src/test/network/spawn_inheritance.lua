-- SPDX-License-Identifier: MPL-2.0

-- process.with_options({network = ...}) must apply the overlay for the full
-- process lifetime. The spawned worker issues a plain http.get with no
-- explicit overlay_network; when inheritance works we observe the broken
-- proxy's dial failure instead of the local app's 200.

local assert = require("assert2")
local time = require("time")

local function main()
	local events_ch = process.events()

	local spawner = process.with_options({
		network = "app.test.network:broken_socks5",
	})
	assert.not_nil(spawner, "with_options returned spawner")

	local worker_pid, err = spawner:spawn_monitored(
		"app.test.network:overlay_worker",
		"app:processes"
	)
	assert.is_nil(err, "spawn no error: " .. tostring(err))
	assert.not_nil(worker_pid, "got worker pid")

	local timeout = time.after("5s")
	local result = channel.select {
		events_ch:case_receive(),
		timeout:case_receive(),
	}
	if result.channel == timeout then
		return false, "timeout waiting for worker exit"
	end

	local event = result.value
	assert.eq(event.kind, process.event.EXIT, "got EXIT event")
	assert.eq(event.from, worker_pid, "EXIT from worker")
	assert.is_nil(event.result.error, "worker did not crash: " .. tostring(event.result.error))

	local payload = event.result.value
	assert.not_nil(payload, "worker returned payload")
	assert.eq(payload.ok, false, "worker must not reach clearnet target through overlay")
	assert.not_nil(payload.err, "worker surfaced proxy error")

	return true
end

return { main = main }
