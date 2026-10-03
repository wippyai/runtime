-- SPDX-License-Identifier: MPL-2.0

-- Test: an eval owned by a function call ends when the call returns, and so
-- does everything the eval spawned.
local assert = require("assert")
local time = require("time")
local funcs = require("funcs")

local function ended(events, pid)
	local ok = process.monitor(pid)
	if not ok then
		return true -- already gone when monitoring started
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
	local pids, err = funcs.call("app.test.eval_module:spawn_and_return")
	assert.is_nil(err, "helper function returns the spawned pids")

	local events = process.events()
	assert.ok(ended(events, pids.eval), "the eval ended with the function call that owned it")
	assert.ok(ended(events, pids.child), "the eval's child ended with it")

	local detached, derr = eval.spawn("return { main = function() end }", {
		detached = true, allow_detached = true,
	})
	assert.is_nil(detached, "a function call cannot detach an eval")
	assert.contains(tostring(derr), "eval detached spawn denied", "detached is refused from function calls")
	return true
end

return { main = main }
