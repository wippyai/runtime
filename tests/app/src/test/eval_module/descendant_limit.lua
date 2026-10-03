-- SPDX-License-Identifier: MPL-2.0

-- Test: max_children bounds the whole tree an eval starts, not only its
-- direct children: a child spawned by the eval draws on the eval's limit.
local assert = require("assert")
local time = require("time")

local function main()
	local events = process.events()
	local child, err = eval.spawn([[
		return { main = function()
			local events = process.events()
			local fan, spawn_err = process.spawn_monitored("app.test.eval_module:fanout", "app:processes")
			if not fan then return { error = tostring(spawn_err) } end
			while true do
				local event = events:receive()
				if event.kind == process.event.EXIT and event.from == fan then
					return event.result.value
				end
			end
		end }
	]], {
		modules = { "process" },
		commands = { "spawn" },
		limits = { max_children = 1 },
		monitor_only = true,
	})
	assert.is_nil(err, "spawn succeeds")

	local timeout = time.after("10s")
	while true do
		local selected = channel.select { events:case_receive(), timeout:case_receive() }
		assert.eq(selected.channel, events, "eval exits")
		if selected.value.kind == process.event.EXIT and selected.value.from == child then
			local result = selected.value.result
			assert.is_nil(result.error, "eval succeeds")
			assert.eq(result.value.started, 0, "a descendant cannot exceed the eval's child limit")
			assert.contains(result.value.refusal, "child limit exceeded", "the limit refuses the spawn")
			return true
		end
	end
end

return { main = main }
