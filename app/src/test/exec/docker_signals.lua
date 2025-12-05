-- Test: Docker container signal handling
local assert = require("assert_primitives")

local SIGTERM = 15
local SIGKILL = 9

local function main()
    local exec = require("exec")

    local executor, err = exec.get("app:exec_docker")
    assert.not_nil(executor, "docker executor acquired")

    -- Create a long-running process in container
    local proc, perr = executor:exec("sleep 30")
    assert.not_nil(proc, "docker process created")

    -- Start it
    local ok, serr = proc:start()
    assert.ok(ok, "docker process started")
    assert.is_nil(serr, "no error starting")

    -- Send SIGTERM signal
    local sigok, sigerr = proc:signal(SIGTERM)
    assert.ok(sigok, "signal sent to docker container")
    assert.is_nil(sigerr, "no signal error")

    -- Wait should complete (process terminated by signal)
    local exitCode, werr = proc:wait()
    assert.not_nil(exitCode, "exit code returned")

    -- Test close with force=false (SIGTERM)
    local proc2, p2err = executor:exec("sleep 30")
    assert.not_nil(proc2, "docker process 2 created")
    proc2:start()

    local closeok, closeerr = proc2:close(false)
    assert.ok(closeok, "docker close succeeds")
    assert.is_nil(closeerr, "docker close no error")

    -- Test close with force=true (SIGKILL)
    local proc3, p3err = executor:exec("sleep 30")
    assert.not_nil(proc3, "docker process 3 created")
    proc3:start()

    local closeok3, closeerr3 = proc3:close(true)
    assert.ok(closeok3, "docker force close succeeds")
    assert.is_nil(closeerr3, "docker force close no error")

    executor:release()

    return true
end

return { main = main }
