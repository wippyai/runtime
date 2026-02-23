-- SPDX-License-Identifier: MPL-2.0

-- Test: Spawn with monitoring and wait for exit
local assert = require("assert2")
local time = require("time")

local function main()
-- Test spawn_monitored exists
	assert.not_nil(process.spawn_monitored, "process.spawn_monitored exists")

	-- Spawn monitored process
	local child_pid, err = process.spawn_monitored("app.test.process:echo_worker", "app:processes", "monitored test")
	assert.is_nil(err, "spawn_monitored no error")
	assert.not_nil(child_pid, "spawn_monitored returns pid")

	-- Give process time to complete
	time.sleep("100ms")

	return true
end

return { main = main }
