-- SPDX-License-Identifier: MPL-2.0

-- Test: Star topology - parent with 10 children that link TO parent
-- When parent errors, all children should receive LINK_DOWN and die
local assert = require("assert2")
local time = require("time")

local function main()
	local events_ch = process.events()

	-- Spawn the parent worker and monitor it
	local parent_pid, err = process.spawn_monitored("app.test.process:star_parent_worker", "app:processes")
	assert.is_nil(err, "spawn parent no error")
	assert.not_nil(parent_pid, "got parent pid")

	-- Wait for parent EXIT event (parent will error after all children link)
	local timeout = time.after("5s")
	local result = channel.select {
		events_ch:case_receive(),
		timeout:case_receive(),
	}

	if result.channel ~= events_ch then
		return false, "timeout waiting for parent exit"
	end

	local event = result.value
	if event.kind ~= process.event.EXIT then
		return false, "expected EXIT event, got: " .. tostring(event.kind)
	end

	if event.from ~= parent_pid then
		return false, "expected event from parent " .. parent_pid .. ", got: " .. tostring(event.from)
	end

	-- Verify parent is dead
	local ok, send_err = process.send(parent_pid, "test", "hello")
	if ok then
		return false, "parent should be dead but send succeeded"
	end

	if not send_err or not string.find(send_err, "not found") then
		return false, "expected 'not found' error, got: " .. tostring(send_err)
	end

	return true
end

return { main = main }
