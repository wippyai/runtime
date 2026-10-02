-- SPDX-License-Identifier: MPL-2.0

-- Test: eval limits bound how long dynamic code may run.
local assert = require("assert")
local time = require("time")

local function main()
	local events = process.events()

	local spinner, err = eval.spawn([[
		return { main = function() while true do end end }
	]], { limits = { tick_budget = 100, max_steps = 5 }, monitor_only = true })
	assert.is_nil(err, "spawn succeeds")

	local timeout = time.after("10s")
	while true do
		local selected = channel.select { events:case_receive(), timeout:case_receive() }
		assert.eq(selected.channel, events, "spinning eval is stopped by max_steps")
		if selected.value.kind == process.event.EXIT and selected.value.from == spinner then
			assert.not_nil(selected.value.result.error, "spinner fails")
			assert.contains(tostring(selected.value.result.error), "step limit exceeded", "max_steps stops the spinner")
			break
		end
	end
	return true
end

return { main = main }
