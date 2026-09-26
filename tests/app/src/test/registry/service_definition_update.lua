-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local registry = require("registry")
local system = require("system")
local time = require("time")

local SERVICE_ID = "app.test.registry:service_definition_update_target"
local NAME = "service_definition_update_target"

local function service_entry(tag, auto_start)
	return {
		id = SERVICE_ID,
		kind = "process.service",
		meta = {
			comment = "process.service definition update regression",
		},
		data = {
			process = "app.test.registry:service_tag_worker",
			host = "app:processes",
			input = { tag, NAME },
			lifecycle = {
				auto_start = auto_start,
			},
		},
	}
end

local function apply(mutate)
	local snap, snap_err = registry.snapshot()
	assert.is_nil(snap_err, "snapshot no error")
	local changes = snap:changes()
	mutate(changes)
	local version, err = changes:apply()
	assert.is_nil(err, "apply changes no error")
	assert.not_nil(version, "apply returns version")
end

-- ping asks the process registered under NAME for its tag. It returns the
-- responding pid and tag, or nil when no process answers within the window.
local function ping(inbox_ch)
	local pid = process.registry.lookup(NAME)
	if not pid then
		return nil
	end
	process.send(pid, "ping", { reply_to = process.pid() })
	local timeout = time.after("200ms")
	local result = channel.select {
		inbox_ch:case_receive(),
		timeout:case_receive(),
	}
	if result.channel ~= inbox_ch then
		return nil
	end
	local data = result.value:payload():data()
	return string(data.pid), string(data.tag)
end

-- await_tag polls until the named process answers with the expected tag.
local function await_tag(inbox_ch, expected)
	local last_tag
	for _ = 1, 100 do
		local pid, tag = ping(inbox_ch)
		if tag == expected then
			return pid
		end
		last_tag = tag
		time.sleep("50ms")
	end
	error("service never answered with tag " .. expected .. " (last: " .. tostring(last_tag) .. ")")
end

local function await_status(expected)
	local last
	for _ = 1, 100 do
		local state = system.supervisor.state(SERVICE_ID)
		last = state and state.status
		if last == expected then
			return
		end
		time.sleep("50ms")
	end
	error("service status never became " .. expected .. " (last: " .. tostring(last) .. ")")
end

local function await_unregistered()
	for _ = 1, 100 do
		if not process.registry.lookup(NAME) then
			return
		end
		time.sleep("50ms")
	end
	error("service process kept its name after the entry was deleted")
end

local function run(inbox_ch)
	apply(function(changes)
		changes:create(service_entry("v1", true))
	end)
	local first_pid = await_tag(inbox_ch, "v1")
	await_status("running")

	-- A changed definition replaces the process. The running service stays
	-- running even though the new definition does not auto-start.
	apply(function(changes)
		changes:update(service_entry("v2", false))
	end)
	local second_pid = await_tag(inbox_ch, "v2")
	assert.ok(second_pid ~= first_pid, "changed definition starts a new process")
	await_status("running")

	-- Re-applying the same definition keeps the running process.
	apply(function(changes)
		changes:update(service_entry("v2", false))
	end)
	local third_pid = await_tag(inbox_ch, "v2")
	assert.eq(third_pid, second_pid, "unchanged definition keeps the process")

	apply(function(changes)
		changes:delete(SERVICE_ID)
	end)
	await_unregistered()
end

local function main()
	local original_version, version_err = registry.current_version()
	assert.is_nil(version_err, "current version no error")

	local ok, err = pcall(run, process.inbox())

	local restored, restore_err = registry.apply_version(original_version)
	assert.is_nil(restore_err, "restore original version")
	assert.ok(restored, "restore original succeeded")

	if not ok then
		error(err)
	end
	return true
end

return { main = main }
