-- Test: process.link explicit function
-- Tests bidirectional linking where worker links to a short-lived target process.
-- Flow:
-- 1. Main spawns a target process that will exit quickly
-- 2. Main spawns worker and tells it to link to target
-- 3. Target exits, triggering LINK_DOWN to worker
-- 4. Worker receives LINK_DOWN and exits with success
-- 5. Main monitors worker and verifies it got LINK_DOWN

local assert = require("assert2")
local time = require("time")

local function main()
-- Test link exists
	assert.not_nil(process.link, "process.link exists")
	assert.is_function(process.link, "process.link is function")

	local events_ch = process.events()

	-- Spawn a target process that will exit after being linked
	local target_pid, err = process.spawn_monitored(
		"app.test.process:link_target",
		"app:processes"
	)
	assert.is_nil(err, "spawn target no error")

	-- Spawn worker that will link to target
	local worker_pid, err2 = process.spawn_monitored(
		"app.test.process:link_explicit_worker",
		"app:processes"
	)
	assert.is_nil(err2, "spawn worker no error")

	-- Send target PID to worker
	process.send(worker_pid, "inbox", target_pid)

	-- Wait for worker to confirm link established
	local inbox_ch = process.inbox()
	local timeout = time.after("2s")
	local result = channel.select {
		inbox_ch:case_receive(),
		timeout:case_receive(),
	}

	if result.channel == timeout then
		return false, "timeout waiting for link confirmation"
	end

	local msg = result.value
	local topic = msg:topic()
	if topic ~= "linked" then
		return false, "expected linked topic, got: " .. tostring(topic)
	end

	-- Tell target to exit now
	process.send(target_pid, "inbox", "exit")

	-- Wait for both processes to exit
	-- First should be target (normal exit)
	-- Second should be worker (after receiving LINK_DOWN)
	local exits = {}
	timeout = time.after("3s")

	for i = 1, 2 do
		result = channel.select {
			events_ch:case_receive(),
			timeout:case_receive(),
		}

		if result.channel == timeout then
			return false, "timeout waiting for exit " .. i .. ", got " .. #exits .. " exits"
		end

		local event = result.value
		if event.kind ~= process.event.EXIT then
			return false, "expected EXIT event, got: " .. tostring(event.kind)
		end

		table.insert(exits, {
			pid = event.from,
			result = event.result
		})
	end

	-- Find worker exit and verify it received LINK_DOWN
	local worker_exit = nil
	for _, exit in ipairs(exits) do
		if exit.pid == worker_pid then
			worker_exit = exit
			break
		end
	end

	if not worker_exit then
		return false, "worker exit not found"
	end

	-- Worker should have returned "LINK_DOWN_RECEIVED" as success result
	-- Result is wrapped in {value=...} table
	local result_value = worker_exit.result
	if type(result_value) == "table" then
		result_value = result_value.value
	end

	if result_value ~= "LINK_DOWN_RECEIVED" then
		local result_str = "nil"
		if worker_exit.result then
			if type(worker_exit.result) == "table" then
				result_str = "{"
				for k, v in pairs(worker_exit.result) do
					result_str = result_str .. tostring(k) .. "=" .. tostring(v) .. ","
				end
				result_str = result_str .. "}"
			else
				result_str = tostring(worker_exit.result)
			end
		end
		return false, "worker should return LINK_DOWN_RECEIVED, got: " .. result_str
	end

	return true
end

return { main = main }
