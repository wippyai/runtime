-- SPDX-License-Identifier: MPL-2.0

-- Test: compiled programs are reusable until evicted.
local assert = require("assert")
local time = require("time")

local function wait_value(events, pid)
	local timeout = time.after("5s")
	while true do
		local selected = channel.select { events:case_receive(), timeout:case_receive() }
		assert.eq(selected.channel, events, "eval process exit received")
		if selected.value.kind == process.event.EXIT and selected.value.from == pid then
			return selected.value.result.value
		end
	end
end

local function main()
	local events = process.events()

	local program, err = eval.compile([[
		return { main = function(x) return x + 1 end }
	]])
	assert.is_nil(err, "compile succeeds")
	assert.eq(tostring(program):sub(1, 13), "eval.Program(", "program prints its identity")

	local again, err2 = eval.compile([[
		return { main = function(x) return x + 1 end }
	]])
	assert.is_nil(err2, "recompile succeeds")
	assert.eq(tostring(again), tostring(program), "same source and policy share a program")

	local first = program:spawn({ input = 1, monitor_only = true })
	local second = eval.spawn(program, { input = 10, monitor_only = true })
	assert.eq(wait_value(events, first), 2, "first spawn of the program")
	assert.eq(wait_value(events, second), 11, "second spawn of the program")

	local mismatch, merr = program:spawn({ modules = { "json" }, monitor_only = true })
	assert.is_nil(mismatch, "spawn with a different policy fails")
	assert.contains(tostring(merr), "eval policy mismatch", "policy mismatch reported")

	local evicted, eerr = program:evict()
	assert.is_nil(eerr, "evict succeeds")
	assert.eq(evicted, true, "evict returns true")

	local gone, gerr = program:spawn({ monitor_only = true })
	assert.is_nil(gone, "spawn after evict fails")
	assert.contains(tostring(gerr), "eval program not found", "evicted program is gone")
	return true
end

return { main = main }
