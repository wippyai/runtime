-- SPDX-License-Identifier: MPL-2.0

-- Test: eval.spawn runs dynamic source as a supervised process.
local assert = require("assert")
local time = require("time")

local function wait_exit(events, pid)
	local timeout = time.after("5s")
	while true do
		local selected = channel.select { events:case_receive(), timeout:case_receive() }
		assert.eq(selected.channel, events, "eval process exit received")
		if selected.value.kind == process.event.EXIT and selected.value.from == pid then
			return selected.value.result
		end
	end
end

local function main()
	local events = process.events()

	local child, err = eval.spawn([[
		return { main = function(x) return x * 2 end }
	]], { input = 21, monitor_only = true })
	assert.is_nil(err, "spawn succeeds")
	assert.not_nil(child, "spawn returns a pid")

	local result = wait_exit(events, child)
	assert.is_nil(result.error, "eval process succeeds")
	assert.eq(result.value, 42, "eval process returns its result")

	local named, nerr = eval.spawn([[
		return { run = function() return "ran" end }
	]], { method = "run", monitor_only = true })
	assert.is_nil(nerr, "spawn with method succeeds")
	result = wait_exit(events, named)
	assert.eq(result.value, "ran", "method selects the entry function")

	local failing, ferr = eval.spawn([[
		return { main = function() error("boom") end }
	]], { monitor_only = true })
	assert.is_nil(ferr, "spawn of failing code succeeds")
	result = wait_exit(events, failing)
	assert.not_nil(result.error, "failure is reported in the exit result")
	return true
end

return { main = main }
