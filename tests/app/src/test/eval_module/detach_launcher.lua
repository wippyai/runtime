-- SPDX-License-Identifier: MPL-2.0

-- Runs as an unowned process and detaches an eval that starts a long-running
-- child, reports the child to target, and returns at once.
local function main(target)
	local pid, err = eval.spawn([[
		return { main = function(target)
			local child, spawn_err = process.spawn("app.test.eval_module:long_sleeper", "app:processes")
			if not child then error(spawn_err) end
			process.send(target, "child", child)
			return "done"
		end }
	]], {
		input = target,
		detached = true,
		allow_detached = true,
		modules = { "process" },
		commands = { "spawn" },
		limits = { max_children = 1 },
		send = "explicit",
		send_targets = { target },
	})
	if not pid then error(err) end
	return pid
end

return { main = main }
