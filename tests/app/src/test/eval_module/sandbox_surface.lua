-- SPDX-License-Identifier: MPL-2.0

-- Test: what eval code can reach. It has no dynamic loading, no debug or
-- host access in the standard library, cannot change tables shared with
-- other processes, and starts nothing that carries more authority than it.
local assert = require("assert")
local time = require("time")

local function wait_exit(events, pid)
	local timeout = time.after("5s")
	while true do
		local selected = channel.select { events:case_receive(), timeout:case_receive() }
		assert.eq(selected.channel, events, "eval process exits")
		if selected.value.kind == process.event.EXIT and selected.value.from == pid then
			assert.is_nil(selected.value.result.error, "eval process succeeds")
			return selected.value.result.value
		end
	end
end

local function run(events: any, source: string, opts: any?)
	opts = opts or {}
	opts.monitor_only = true
	local pid, err = eval.spawn(source, opts)
	assert.is_nil(err, "spawn succeeds")
	return wait_exit(events, pid)
end

local function main()
	local events = process.events()

	local surface = run(events, [[
		return { main = function()
			local found = {}
			for _, name in ipairs({ "load", "loadstring", "loadfile", "dofile", "getfenv", "setfenv",
				"debug", "io", "newproxy", "collectgarbage", "module" }) do
				if rawget(_G, name) ~= nil then found[#found + 1] = name end
			end
			for name in pairs(os) do
				if name ~= "clock" and name ~= "date" and name ~= "difftime" and name ~= "platform" and name ~= "time" then
					found[#found + 1] = "os." .. name
				end
			end
			for _, name in ipairs({ "funcs", "registry", "env", "io", "fs", "eval", "exec", "sql", "store", "debug", "security" }) do
				if pcall(require, name) then found[#found + 1] = "require " .. name end
			end
			return table.concat(found, ",")
		end }
	]], { modules = { "process", "time", "json" } })
	assert.eq(surface, "", "no ambient surface in the standard library")

	local immutable = run(events, [[
		return { main = function()
			local shared = {
				function() json.encode = nil end,
				function() process.send = nil end,
				function() time.sleep = nil end,
				function() string.rep = nil end,
				function() table.insert = nil end,
				function() math.floor = nil end,
				function() getmetatable("").__index = {} end,
				function() setmetatable(string, {}) end,
				function() getmetatable(process.events()).__index = {} end,
				function() getmetatable(time.now()).__index.unix = nil end,
				function() local _, e = process.send("{x|y}", "t"); getmetatable(e).__index = {} end,
			}
			local mutable = {}
			for i, mutate in ipairs(shared) do
				if pcall(mutate) then mutable[#mutable + 1] = tostring(i) end
			end
			return table.concat(mutable, ",")
		end }
	]], { modules = { "process", "time", "json" } })
	assert.eq(immutable, "", "tables shared with other processes cannot be changed")

	local writer = [[
		return { main = function()
			LEAK = "set"
			_G.LEAK2 = "set"
			return "wrote"
		end }
	]]
	local reader = [[
		return { main = function()
			return tostring(LEAK) .. tostring(LEAK2) .. tostring(rawget(_G, "LEAK"))
		end }
	]]
	assert.eq(run(events, writer), "wrote", "writer runs")
	assert.eq(run(events, reader), "nilnilnil", "globals do not cross processes")

	local shared_table = { list = { 1, 2 }, name = "caller" }
	local program, cerr = eval.compile([[
		return { main = function()
			local before = list[1] .. name.x
			list[1] = 99
			name.x = "changed"
			return before
		end }
	]], { bindings = { list = shared_table.list, name = { x = "orig" } } })
	assert.is_nil(cerr, "compile succeeds")
	local first, serr = program:spawn({ monitor_only = true })
	assert.is_nil(serr, "program spawns")
	assert.eq(wait_exit(events, first), "1orig", "first run sees the admitted bindings")
	local second = program:spawn({ monitor_only = true })
	assert.eq(wait_exit(events, second), "1orig", "a run does not change bindings of the cached program")
	assert.eq(shared_table.list[1], 1, "the caller's table is untouched")

	local probe = [[return { main = function() return type(process) .. type(json) end }]]
	assert.eq(run(events, probe, { modules = { "process", "json" } }), "tabletable", "admitted modules are present")
	assert.eq(run(events, probe), "nilnil", "the same source under another policy gets only that policy's modules")
	assert.eq(run(events, probe, { modules = { "json" } }), "niltable", "a policy admits exactly its modules")

	local import_opts = { imports = { lib = "app.lib:assert" } }
	assert.eq(run(events, [[
		return { main = function() lib.eq = "tampered"; lib.added = true; return "wrote" end }
	]], import_opts), "wrote", "an eval changes its own copy of an import")
	assert.eq(run(events, [[
		return { main = function() return type(lib.eq) .. tostring(lib.added) end }
	]], import_opts), "functionnil", "imports are not shared between processes")

	local inbox = process.inbox()
	local victim_pid, verr = process.spawn_monitored("app.test.eval_module:long_sleeper", "app:processes")
	assert.is_nil(verr, "victim starts")
	assert.ok(process.registry.register("eval_surface.victim", victim_pid), "victim is registered under a name")
	local explicit = run(events, [[
		return { main = function(arg)
			local looked_up = process.registry.lookup("eval_surface.victim")
			local to_victim, victim_err = process.send(looked_up, "pwn", "x")
			local to_self, self_err = process.send(process.pid(), "pwn", "x")
			local to_target, target_err = process.send(arg.target, "ok", "x")
			return {
				victim = to_victim == true and "sent" or tostring(victim_err),
				self = to_self == true and "sent" or tostring(self_err),
				target = to_target == true and "sent" or tostring(target_err),
			}
		end }
	]], {
		modules = { "process" },
		commands = { "lookup" },
		send = "explicit",
		send_targets = { process.pid() },
		input = { target = process.pid() },
	})
	assert.contains(explicit.victim, "not allowed", "explicit mode refuses a PID found by lookup")
	assert.contains(explicit.self, "not allowed", "explicit mode refuses the eval itself")
	assert.eq(explicit.target, "sent", "explicit mode allows its targets")
	local received = channel.select { inbox:case_receive(), time.after("2s"):case_receive() }
	assert.eq(received.value:topic(), "ok", "only the explicit target received a message")
	process.terminate(victim_pid)

	local report = run(events, [[
		return { main = function()
			local events = process.events()
			local child, err = process.spawn_monitored("app.test.eval_module:privileged_child", "app:processes")
			if not child then return { error = tostring(err) } end
			while true do
				local event = events:receive()
				if event.kind == process.event.EXIT and event.from == child then
					return event.result.error and { error = tostring(event.result.error) } or event.result.value
				end
			end
		end }
	]], { modules = { "process" }, commands = { "spawn" }, limits = { max_children = 1 } })
	assert.is_nil(report.error, "child runs")
	assert.contains(report.funcs, "not allowed", "a child's configured policy does not widen the eval's authority: funcs")
	assert.contains(report.registry, "not allowed", "registry")
	assert.contains(report.env, "not allowed", "env")
	return true
end

return { main = main }
