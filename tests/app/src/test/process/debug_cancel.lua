-- SPDX-License-Identifier: MPL-2.0

-- Debug test: Verify cancel delivery path
local time = require("time")

local function main()
	io.write("DEBUG: Starting debug_cancel test\n")
	io.flush()

	local events_ch = process.events()
	io.write("DEBUG: Got events channel\n")
	io.flush()

	-- Spawn a child that waits for cancel
	local child_pid, err = process.spawn_monitored("app.test.process:debug_cancel_worker", "app:processes")
	if err then
		return false, "spawn failed: " .. tostring(err)
	end
	io.write("DEBUG: Spawned child: " .. tostring(child_pid) .. "\n")
	io.flush()

	-- Small delay to ensure child is ready
	time.sleep("10ms")
	io.write("DEBUG: Sending cancel to child\n")
	io.flush()

	-- Cancel the child
	local ok, cancel_err = process.cancel(child_pid)
	if cancel_err then
		return false, "cancel failed: " .. tostring(cancel_err)
	end
	io.write("DEBUG: Cancel sent, ok=" .. tostring(ok) .. "\n")
	io.flush()

	-- Wait for EXIT from child (with timeout)
	io.write("DEBUG: Waiting for EXIT event\n")
	io.flush()

	local timeout = time.after("3s")
	local result = channel.select {
		events_ch:case_receive(),
		timeout:case_receive(),
	}

	if result.channel ~= events_ch then
		io.write("DEBUG: Timeout! No EXIT event received\n")
		io.flush()
		return false, "timeout waiting for EXIT"
	end

	local event = result.value
	io.write("DEBUG: Got event kind=" .. tostring(event.kind) .. " from=" .. tostring(event.from) .. "\n")
	io.flush()

	if event.kind ~= process.event.EXIT then
		return false, "expected EXIT, got: " .. tostring(event.kind)
	end

	return true
end

return { main = main }
