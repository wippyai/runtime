-- SPDX-License-Identifier: MPL-2.0

-- Test: eval code resolves registered names only when its policy allows the
-- lookup command; a resolved PID is then addressable like any granted PID.
local assert = require("assert")
local time = require("time")

local lookup_and_send = [[
	return { main = function()
		local found, lookup_err = process.registry.lookup("eval_lookup.victim")
		if not found then return { lookup = tostring(lookup_err) } end
		local ok, send_err = process.send(found, "found", "x")
		return { lookup = "found", send = ok == true and "sent" or tostring(send_err) }
	end }
]]

local function run(events: any, opts: any)
	opts.monitor_only = true
	local pid, err = eval.spawn(lookup_and_send, opts)
	assert.is_nil(err, "spawn succeeds")
	local timeout = time.after("5s")
	while true do
		local selected = channel.select { events:case_receive(), timeout:case_receive() }
		assert.eq(selected.channel, events, "eval exits")
		if selected.value.kind == process.event.EXIT and selected.value.from == pid then
			assert.is_nil(selected.value.result.error, "eval process succeeds")
			return selected.value.result.value
		end
	end
end

local function main()
	local events = process.events()
	local inbox = process.inbox()
	local victim, verr = process.spawn_monitored("app.test.eval_module:long_sleeper", "app:processes")
	assert.is_nil(verr, "victim starts")
	assert.ok(process.registry.register("eval_lookup.victim", victim), "victim is registered")
	local function delivered()
		local selected = channel.select { inbox:case_receive(), time.after("300ms"):case_receive() }
		return selected.channel == inbox
	end

	local denied = run(events, { modules = { "process" } })
	assert.contains(denied.lookup, "not allowed", "lookup is denied by default")
	assert.ok(not delivered(), "nothing reaches the victim")

	local explicit = run(events, { modules = { "process" }, commands = { "lookup" }, send = "explicit", send_targets = { process.pid() } })
	assert.eq(explicit.lookup, "found", "an allowed lookup resolves the name")
	assert.contains(explicit.send, "not allowed", "explicit mode still refuses a PID that is not a target")

	local allowed = run(events, { modules = { "process" }, commands = { "lookup" } })
	assert.eq(allowed.lookup, "found", "an allowed lookup resolves the name")
	assert.eq(allowed.send, "sent", "the resolved PID is addressable in capability mode")

	assert.eq(process.registry.lookup("eval_lookup.victim"), victim, "ordinary processes look names up freely")
	process.terminate(victim)
	return true
end

return { main = main }
