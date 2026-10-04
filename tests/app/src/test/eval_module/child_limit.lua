-- SPDX-License-Identifier: MPL-2.0

-- Test: max_children bounds running children; a slot frees when a child exits.
local assert = require("assert")
local time = require("time")

local function main()
	local events = process.events()

	local child, err = eval.spawn([[
		return { main = function()
			local events = process.events()
			local first, first_err = process.spawn_monitored("app.test.eval_module:sleeper", "app:processes")
			if not first then return "first spawn failed: " .. tostring(first_err) end
			local second, second_err = process.spawn("app.test.eval_module:sleeper", "app:processes")
			if second then return "second spawn should exceed the limit" end
			local limited = tostring(second_err)
			while true do
				local event = events:receive()
				if event.kind == process.event.EXIT and event.from == first then break end
			end
			local third, third_err = process.spawn("app.test.eval_module:sleeper", "app:processes")
			if not third then return "spawn after exit failed: " .. tostring(third_err) end
			return limited
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
		assert.eq(selected.channel, events, "eval process exits")
		if selected.value.kind == process.event.EXIT and selected.value.from == child then
			assert.is_nil(selected.value.result.error, "eval process succeeds")
			assert.contains(selected.value.result.value, "child limit exceeded", "second spawn hits the limit")
			break
		end
	end

	local no_spawn, nerr = eval.spawn([[
		return { main = function()
			local pid, spawn_err = process.spawn("app.test.eval_module:sleeper", "app:processes")
			return tostring(spawn_err)
		end }
	]], { modules = { "process" }, monitor_only = true })
	assert.is_nil(nerr, "spawn without commands succeeds")
	timeout = time.after("5s")
	while true do
		local selected = channel.select { events:case_receive(), timeout:case_receive() }
		assert.eq(selected.channel, events, "eval without spawn command exits")
		if selected.value.kind == process.event.EXIT and selected.value.from == no_spawn then
			assert.contains(selected.value.result.value, "not allowed", "spawning requires the spawn command")
			break
		end
	end
	return true
end

return { main = main }
