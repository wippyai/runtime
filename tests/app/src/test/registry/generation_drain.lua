-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local events = require("events")
local funcs = require("funcs")
local registry = require("registry")
local time = require("time")

local slow_id = "app.test.registry:generation_drain_slow"
local waiter_id = "app.test.registry:generation_drain_waiter"
local event_system = "app.test.registry.generation_drain"

local function slow_entry()
	return {
		id = slow_id,
		kind = "function.lua",
		meta = {
			comment = "slow callee for generation drain regression",
		},
		data = {
			source = [=[
local time = require("time")

local function main(delay_ms, value)
	time.sleep(delay_ms or 50)
	return { value = value or "done" }
end

return { main = main }
]=],
			method = "main",
			modules = { "time" },
		},
	}
end

local function waiter_entry(version)
	return {
		id = waiter_id,
		kind = "function.lua",
		meta = {
			comment = "caller generation drain regression",
		},
		data = {
			source = ([=[
local events = require("events")
local funcs = require("funcs")

local function main(delay_ms, value)
	events.send("%s", "waiter.started", "/started", { version = "%s", value = value })
	local future, err = funcs.async("%s", delay_ms or 50, value)
	if err ~= nil then
		return { version = "%s", error = tostring(err) }
	end
	local payload, ok = future:response():receive()
	if not ok then
		return { version = "%s", error = "missing async response" }
	end
	return { version = "%s", value = payload:data().value }
end

return { main = main }
]=]):format(event_system, version, slow_id, version, version, version),
			method = "main",
			modules = { "events", "funcs" },
		},
	}
end

local function entry_exists(id)
	local entry = registry.get(id)
	return entry ~= nil
end

local function new_changes()
	local snap, err = registry.snapshot()
	assert.is_nil(err, "snapshot no error")
	assert.not_nil(snap, "snapshot available")
	return snap:changes()
end

local function apply_changes(apply_fn)
	local changes = new_changes()
	local count = apply_fn(changes) or 0
	assert.ok(count > 0, "apply_changes requires at least one operation")
	local version, err = changes:apply()
	assert.is_nil(err, "apply no error")
	assert.not_nil(version, "version returned")
	return version
end

local function delete_leftovers()
	local changes = new_changes()
	local count = 0
	if entry_exists(waiter_id) then
		changes:delete(waiter_id)
		count = count + 1
	end
	if entry_exists(slow_id) then
		changes:delete(slow_id)
		count = count + 1
	end
	if count > 0 then
		local _, err = changes:apply()
		assert.is_nil(err, "leftover delete no error")
	end
end

local function receive_from(channel_value, message)
	local timer = time.after(1000 * time.MILLISECOND)
	local selected = channel.select{
		channel_value:case_receive(),
		timer:case_receive(),
	}
	assert.eq(selected.channel, channel_value, message)
	assert.eq(selected.ok, true, message .. " ok")
	return selected.value
end

local function main()
	delete_leftovers()

	local sub, sub_err = events.subscribe(event_system)
	assert.is_nil(sub_err, "subscribe no error")
	assert.not_nil(sub, "subscription created")
	local start_ch = sub:channel()

	apply_changes(function(changes)
		changes:create(slow_entry())
		changes:create(waiter_entry("old"))
		return 2
	end)

	local old_future, old_err = funcs.async(waiter_id, 250, "old-call")
	assert.is_nil(old_err, "old waiter async start no error")
	assert.not_nil(old_future, "old waiter future")

	local started = receive_from(start_ch, "old waiter started before replacement")
	assert.eq(started.data.version, "old", "old generation started")

	apply_changes(function(changes)
		changes:update(waiter_entry("new"))
		return 1
	end)

	local old_payload = receive_from(old_future:channel(), "old generation received async reply after replacement")
	assert.eq(old_payload:data().version, "old", "old generation completed")
	assert.eq(old_payload:data().value, "old-call", "old generation returned callee result")

	local new_result, new_err = funcs.call(waiter_id, 10, "new-call")
	assert.is_nil(new_err, "new waiter call no error")
	assert.eq(new_result.version, "new", "new generation is active")
	assert.eq(new_result.value, "new-call", "new generation returned callee result")

	sub:close()
	delete_leftovers()
	return true
end

return { main = main }
