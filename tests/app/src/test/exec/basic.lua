-- Test: Basic process execution
local assert = require("assert_primitives")

local function main()
	local exec = require("exec")

	-- Acquire executor
	local executor, err = exec.get("app:exec")
	assert.not_nil(executor, "executor acquired")
	assert.is_nil(err, "no error acquiring executor")

	-- Create process
	local proc, perr = executor:exec("echo hello")
	assert.not_nil(proc, "process created")
	assert.is_nil(perr, "no error creating process")

	-- Start process
	local ok, serr = proc:start()
	assert.ok(ok, "process started")
	assert.is_nil(serr, "no error starting")

	-- Wait for completion
	local exitCode, werr = proc:wait()
	assert.is_nil(werr, "no error waiting")
	assert.eq(exitCode, 0, "exit code is 0")

	-- Test non-zero exit code
	local proc2, _ = executor:exec("sh -c 'exit 42'")
	assert.not_nil(proc2, "process 2 created")

	proc2:start()
	local exitCode2, w2err = proc2:wait()
	assert.is_nil(w2err, "no error waiting for exit 42")
	assert.eq(exitCode2, 42, "exit code is 42")

	-- Test process with env
	local proc3, _ = executor:exec("sh -c 'echo $TEST_VAR'", {
		env = { TEST_VAR = "test_value" }
	})
	assert.not_nil(proc3, "process 3 created with env")

	proc3:start()
	local exitCode3, _ = proc3:wait()
	assert.eq(exitCode3, 0, "env process exits 0")

	-- Test process with work_dir
	local proc4, _ = executor:exec("pwd", {
		work_dir = "/tmp"
	})
	assert.not_nil(proc4, "process 4 created with work_dir")

	proc4:start()
	local exitCode4, _ = proc4:wait()
	assert.eq(exitCode4, 0, "work_dir process exits 0")

	-- Release executor
	executor:release()

	return true
end

return { main = main }
