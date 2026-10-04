-- SPDX-License-Identifier: MPL-2.0

-- Test: processes a detached eval starts end with the eval.
local assert = require("assert")
local time = require("time")

local function main()
	local events = process.events()
	local inbox = process.inbox()
	local launcher, err = process.spawn_monitored("app.test.eval_module:detach_launcher", "app:processes", process.pid())
	assert.is_nil(err, "launcher starts")

	local timeout = time.after("10s")
	local child
	while not child do
		local selected = channel.select { inbox:case_receive(), timeout:case_receive() }
		assert.eq(selected.channel, inbox, "the detached eval reports its child")
		if selected.value:topic() == "child" then
			child = selected.value:payload():data()
		end
	end

	local watching = process.monitor(child)
	if watching then
		timeout = time.after("3s")
		while true do
			local selected = channel.select { events:case_receive(), timeout:case_receive() }
			assert.eq(selected.channel, events, "the child ended with the detached eval")
			if selected.value.kind == process.event.EXIT and selected.value.from == child then
				break
			end
		end
	end
	return true
end

return { main = main }
