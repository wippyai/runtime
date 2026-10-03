-- SPDX-License-Identifier: MPL-2.0

-- Spawns an eval that starts a long-running child of its own, waits until
-- the eval reports that child, then returns at once with both PIDs.
local function main()
	local inbox = process.inbox()
	local pid, err = eval.spawn([[
		return { main = function(parent)
			local child, spawn_err = process.spawn("app.test.eval_module:long_sleeper", "app:processes")
			if not child then error(spawn_err) end
			process.send(parent, "child", child)
			local time = require("time")
			time.sleep("10s")
			return "outlived its caller"
		end }
	]], {
		input = process.pid(),
		modules = { "process", "time" },
		commands = { "spawn" },
		limits = { max_children = 1 },
	})
	if err then error(err) end
	local msg = inbox:receive()
	return { eval = pid, child = msg:payload():data() }
end

return { main = main }
