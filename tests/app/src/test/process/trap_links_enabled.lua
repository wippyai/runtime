-- Test: With trap_links=true, process receives LINK_DOWN event
-- This tests the spec requirement: "link down events will only arrive
-- if the trap_links option is set to true"

local assert = require("assert2")
local time = require("time")

local function main()
	local events_ch = process.events()

	-- Spawn a monitored worker that sets trap_links=true
	local worker_pid, err = process.spawn_monitored(
		"app.test.process:trap_links_enabled_worker",
		"app:processes"
	)
	assert.is_nil(err, "spawn worker no error")

	-- Wait for worker EXIT event
	local timeout = time.after("3s")
	local result = channel.select {
		events_ch:case_receive(),
		timeout:case_receive(),
	}

	if result.channel == timeout then
		return false, "timeout waiting for worker exit"
	end

	local event = result.value
	assert.eq(event.kind, process.event.EXIT, "got EXIT event")
	assert.eq(event.from, worker_pid, "event from worker")

	-- Worker should have returned success with "LINK_DOWN_RECEIVED"
	local result_value = event.result
	if type(result_value) == "table" then
		result_value = result_value.value
	end

	if result_value ~= "LINK_DOWN_RECEIVED" then
		local result_str = "nil"
		if event.result then
			if type(event.result) == "table" then
				result_str = "{"
				for k, v in pairs(event.result) do
					result_str = result_str .. tostring(k) .. "=" .. tostring(v) .. ","
				end
				result_str = result_str .. "}"
			else
				result_str = tostring(event.result)
			end
		end
		return false, "worker should return LINK_DOWN_RECEIVED, got: " .. result_str
	end

	return true
end

return { main = main }
