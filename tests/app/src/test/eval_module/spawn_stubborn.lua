-- SPDX-License-Identifier: MPL-2.0

-- Spawns an eval that resists ending in every way it can, waits until it
-- reports its child, then returns at once with both PIDs.
local function main()
	local inbox = process.inbox()
	local pid, err = eval.spawn([[
		return { main = function(parent)
			process.set_options({ trap_links = true, upgradable = true })
			pcall(process.upgrade)
			pcall(process.upgrade, "app.test.eval_module:stubborn_child")
			local child = process.spawn_linked("app.test.eval_module:stubborn_child", "app:processes")
			process.send(parent, "child", child)
			local events = process.events()
			while true do
				pcall(function() events:receive() end)
			end
		end }
	]], {
		input = process.pid(),
		modules = { "process" },
		commands = { "spawn" },
		limits = { max_children = 1 },
	})
	if err then error(err) end
	local msg = inbox:receive()
	return { eval = pid, child = msg:payload():data() }
end

return { main = main }
