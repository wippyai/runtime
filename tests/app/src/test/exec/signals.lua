-- Test: Process signal handling
local assert = require("assert_primitives")

local SIGTERM = 15

local function main()
	local exec = require("exec")

	local executor, _ = exec.get("app:exec")
	assert.not_nil(executor, "executor acquired")

	-- Create a long-running process
	local proc, _ = executor:exec("sleep 30")
	assert.not_nil(proc, "process created")

	-- Start it
	local ok, serr = proc:start()
	assert.ok(ok, "process started")
	assert.is_nil(serr, "no error starting")

	-- Send SIGTERM signal
	local sigok, sigerr = proc:signal(SIGTERM)
	assert.ok(sigok, "signal sent")
	assert.is_nil(sigerr, "no signal error")

	-- Wait should complete (process terminated by signal)
	local exitCode, _ = proc:wait()
	-- Exit code for signal is typically 128+signal or negative
	assert.not_nil(exitCode, "exit code returned")

	-- Test close with force=false (SIGTERM)
	local proc2, _ = executor:exec("sleep 30")
	assert.not_nil(proc2, "process 2 created")
	proc2:start()

	local closeok, closeerr = proc2:close(false)
	assert.ok(closeok, "close succeeds")
	assert.is_nil(closeerr, "close no error")

	-- Test close with force=true (SIGKILL)
	local proc3, _ = executor:exec("sleep 30")
	assert.not_nil(proc3, "process 3 created")
	proc3:start()

	local closeok3, closeerr3 = proc3:close(true)
	assert.ok(closeok3, "force close succeeds")
	assert.is_nil(closeerr3, "force close no error")

	-- Operations on closed process
	local op1, op1err = proc3:start()
	assert.is_nil(op1, "start on closed returns nil")
	assert.not_nil(op1err, "start on closed returns error")

	local op2, op2err = proc3:signal(SIGTERM)
	assert.is_nil(op2, "signal on closed returns nil")
	assert.not_nil(op2err, "signal on closed returns error")

	executor:release()

	return true
end

return { main = main }
