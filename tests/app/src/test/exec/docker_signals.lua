-- SPDX-License-Identifier: MPL-2.0

-- Test: Docker container signal handling
local assert = require("assert_primitives")

local SIGKILL = 9

local function main()
	local exec = require("exec")

	local executor, _ = exec.get("app:exec_docker")
	assert.not_nil(executor, "docker executor acquired")

	-- Create a long-running process
	local proc, _ = executor:exec("sleep 30")
	assert.not_nil(proc, "docker process created")

	-- Start it
	local ok, serr = proc:start()
	assert.ok(ok, "docker process started")
	assert.is_nil(serr, "no error starting")

	-- Send SIGKILL signal (instant termination)
	local sigok, sigerr = proc:signal(SIGKILL)
	assert.ok(sigok, "signal sent to docker container")
	assert.is_nil(sigerr, "no signal error")

	-- Wait should complete (process terminated by signal)
	local exitCode, _ = proc:wait()
	assert.not_nil(exitCode, "exit code returned")

	-- Test close with force=true (SIGKILL)
	local proc2, _ = executor:exec("sleep 30")
	assert.not_nil(proc2, "docker process 2 created")
	proc2:start()

	local closeok, closeerr = proc2:close(true)
	assert.ok(closeok, "docker close succeeds")
	assert.is_nil(closeerr, "docker close no error")

	executor:release()

	return true
end

return { main = main }
