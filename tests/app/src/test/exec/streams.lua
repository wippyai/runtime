-- Test: Process stdout/stderr streams
local assert = require("assert_primitives")

local function main()
	local exec = require("exec")

	local executor, _ = exec.get("app:exec")
	assert.not_nil(executor, "executor acquired")

	-- Test stdout with read
	local proc, _ = executor:exec("echo 'hello stdout'")
	assert.not_nil(proc, "process created")

	-- Get stdout stream before start
	local stdout, serr = proc:stdout_stream()
	assert.not_nil(stdout, "stdout stream acquired")
	assert.is_nil(serr, "no error getting stdout")

	-- Verify stream has read method (Stream type)
	assert.not_nil(stdout.read, "stdout has read method")
	assert.not_nil(stdout.close, "stdout has close method")

	proc:start()

	-- Read from stdout via dispatcher (before wait, while process is running)
	local data, rerr = stdout:read()
	assert.is_nil(rerr, "no error reading stdout")
	assert.not_nil(data, "data read from stdout")

	proc:wait()
	stdout:close()

	-- Test stderr
	local proc2, _ = executor:exec("sh -c 'echo error >&2'")
	assert.not_nil(proc2, "process 2 created")

	local stderr, stderr_err = proc2:stderr_stream()
	assert.not_nil(stderr, "stderr stream acquired")
	assert.is_nil(stderr_err, "no error getting stderr")

	proc2:start()
	proc2:wait()

	-- Test write_stdin
	local proc3, _ = executor:exec("cat")
	assert.not_nil(proc3, "cat process created")

	proc3:start()

	local wok, werr = proc3:write_stdin("test input\n")
	assert.ok(wok, "write_stdin succeeds")
	assert.is_nil(werr, "write_stdin no error")

	proc3:close(true)

	-- Test streams not available on closed process
	local proc4, _ = executor:exec("echo test")
	assert.not_nil(proc4, "process 4 created")

	proc4:start()
	proc4:wait()
	proc4:close()

	local stdout4, s4err = proc4:stdout_stream()
	assert.is_nil(stdout4, "stdout on closed returns nil")
	assert.not_nil(s4err, "stdout on closed returns error")

	executor:release()

	return true
end

return { main = main }
