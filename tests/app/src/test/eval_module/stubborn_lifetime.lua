-- SPDX-License-Identifier: MPL-2.0

-- Test: an eval that traps links, retries upgrades and ignores every event
-- still ends with the execution that owns it, and so does its child.
local assert = require("assert")
local time = require("time")
local funcs = require("funcs")

local function ended(events, pid)
	if not process.monitor(pid) then
		return true
	end
	local timeout = time.after("3s")
	while true do
		local selected = channel.select { events:case_receive(), timeout:case_receive() }
		if selected.channel ~= events then
			return false
		end
		if selected.value.kind == process.event.EXIT and selected.value.from == pid then
			return true
		end
	end
end

local function main()
	local pids, err = funcs.call("app.test.eval_module:spawn_stubborn")
	assert.is_nil(err, "helper function returns the spawned pids")

	local events = process.events()
	assert.ok(ended(events, pids.eval), "the stubborn eval ended with its owner")
	assert.ok(ended(events, pids.child), "its child ended with it")
	return true
end

return { main = main }
