-- Test: Exec module error handling
local assert = require("assert_primitives")

local function main()
    local exec = require("exec")

    -- exec.get with empty ID
    local e, err = exec.get("")
    assert.is_nil(e, "empty id returns nil")
    assert.not_nil(err, "empty id returns error")

    -- exec.get with non-existent resource
    local e2, err2 = exec.get("nonexistent:resource")
    assert.is_nil(e2, "nonexistent resource returns nil")
    assert.not_nil(err2, "nonexistent resource returns error")

    -- get valid executor for further tests
    local executor, gerr = exec.get("app:exec")
    assert.not_nil(executor, "executor acquired")
    assert.is_nil(gerr, "no error acquiring executor")

    -- executor:exec with empty command
    local p, perr = executor:exec("")
    assert.is_nil(p, "empty command returns nil")
    assert.not_nil(perr, "empty command returns error")

    -- executor:release
    local ok, rerr = executor:release()
    assert.ok(ok, "release succeeds")
    assert.is_nil(rerr, "release no error")

    -- executor:exec after release
    local p2, perr2 = executor:exec("echo test")
    assert.is_nil(p2, "exec after release returns nil")
    assert.not_nil(perr2, "exec after release returns error")

    -- double release is ok
    local ok2, rerr2 = executor:release()
    assert.ok(ok2, "double release succeeds")
    assert.is_nil(rerr2, "double release no error")

    return true
end

return { main = main }
