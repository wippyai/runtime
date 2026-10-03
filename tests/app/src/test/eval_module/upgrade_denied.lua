-- SPDX-License-Identifier: MPL-2.0

-- Test: eval code cannot replace itself with a registry process, which would
-- shed its admission policy, budgets and ownership.
local assert = require("assert")
local time = require("time")

local function main()
	local events = process.events()
	local child, err = eval.spawn([[
		return { main = function()
			local _, upgrade_err = process.upgrade("app.test.eval_module:sleeper")
			return tostring(upgrade_err)
		end }
	]], { modules = { "process" }, monitor_only = true })
	assert.is_nil(err, "spawn succeeds")

	local timeout = time.after("5s")
	while true do
		local selected = channel.select { events:case_receive(), timeout:case_receive() }
		assert.eq(selected.channel, events, "eval exits")
		if selected.value.kind == process.event.EXIT and selected.value.from == child then
			local result = selected.value.result
			assert.is_nil(result.error, "eval process succeeds")
			assert.contains(result.value, "not allowed to upgrade", "upgrade is refused")
			return true
		end
	end
end

return { main = main }
