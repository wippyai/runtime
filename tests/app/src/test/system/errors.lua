local assert = require("assert_primitives")

local function main()
    local system = require("system")

    -- Test memory.set_limit with invalid arg
    local result, err = system.memory.set_limit(-5)
    assert.is_nil(result, "set_limit -5 returns nil")
    assert.not_nil(err, "set_limit -5 returns error")
    assert.eq(err:kind(), errors.INVALID, "error kind is INVALID")

    -- Test max_procs with invalid arg
    local result, err = system.runtime.max_procs(0)
    assert.is_nil(result, "max_procs 0 returns nil")
    assert.not_nil(err, "max_procs 0 returns error")
    assert.eq(err:kind(), errors.INVALID, "error kind is INVALID")
    assert.eq(err:retryable(), false, "error not retryable")

    local result, err = system.runtime.max_procs(-1)
    assert.is_nil(result, "max_procs -1 returns nil")
    assert.not_nil(err, "max_procs -1 returns error")
    assert.eq(err:kind(), errors.INVALID, "error kind is INVALID")

    return true
end

return { main = main }
