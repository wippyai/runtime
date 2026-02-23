-- SPDX-License-Identifier: MPL-2.0

-- Test: Chain topology - A -> B -> C -> D -> E -> F
-- Each node links to its parent. When root errors, cascade kills all.
local assert = require("assert2")
local time = require("time")

local function main()
	local events_ch = process.events()
	assert.not_nil(events_ch, "got events channel")

	-- Spawn the chain root
	local root_pid, err = process.spawn_monitored("app.test.process:chain_root_worker", "app:processes")
	assert.is_nil(err, "spawn root no error")
	assert.not_nil(root_pid, "got root pid")

	-- Wait for EXIT event from root
	local timeout = time.after("5s")
	local result = channel.select {
		events_ch:case_receive(),
		timeout:case_receive(),
	}

	if result.channel ~= events_ch then
		return false, "timeout waiting for root EXIT event"
	end

	local event = result.value
	assert.eq(event.kind, process.event.EXIT, "got EXIT event")
	assert.eq(event.from, root_pid, "event from root")

	-- Try to send to root - should fail (process not found)
	local ok, send_err = process.send(root_pid, "test", "hello")
	if ok then
		return false, "root should be dead but send succeeded"
	end

	-- Verify error mentions process not found
	if not send_err or not string.find(send_err, "not found") then
		return false, "expected 'not found' error, got: " .. tostring(send_err)
	end

	return true
end

return { main = main }
