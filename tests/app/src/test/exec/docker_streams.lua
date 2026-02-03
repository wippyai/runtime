-- Test: Docker container stdout/stderr streams
local assert = require("assert_primitives")

local function main()
	local exec = require("exec")

	local executor, _ = exec.get("app:exec_docker")
	assert.not_nil(executor, "docker executor acquired")

	-- Test stdout (must start first for docker, streams are created on Start)
	local proc, _ = executor:exec("echo 'hello stdout'")
	assert.not_nil(proc, "docker process created")

	proc:start()

	-- Get stdout stream after start
	local stdout, serr = proc:stdout_stream()
	assert.not_nil(stdout, "docker stdout stream acquired")
	assert.is_nil(serr, "no error getting stdout")

	proc:wait()

	-- Test stderr
	local proc2, _ = executor:exec("sh -c 'echo error >&2'")
	assert.not_nil(proc2, "docker process 2 created")

	proc2:start()

	local stderr, stderr_err = proc2:stderr_stream()
	assert.not_nil(stderr, "docker stderr stream acquired")
	assert.is_nil(stderr_err, "no error getting stderr")

	proc2:wait()

	-- Test write_stdin
	local proc3, _ = executor:exec("cat")
	assert.not_nil(proc3, "docker cat process created")

	proc3:start()

	local wok, werr = proc3:write_stdin("test input\n")
	assert.ok(wok, "docker write_stdin succeeds")
	assert.is_nil(werr, "docker write_stdin no error")

	proc3:close(true)

	executor:release()

	return true
end

return { main = main }
