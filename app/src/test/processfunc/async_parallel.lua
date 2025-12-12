-- Test: Parallel async calls to process functions
local assert = require("assert2")
local funcs = require("funcs")
local time = require("time")

local function main()
    local start = time.now()

    -- Start multiple async process calls in parallel
    local f1, err1 = funcs.async("app.test.processfunc:slow_process", "50ms")
    local f2, err2 = funcs.async("app.test.processfunc:slow_process", "50ms")
    local f3, err3 = funcs.async("app.test.processfunc:slow_process", "50ms")

    assert.is_nil(err1, "async 1 no error")
    assert.is_nil(err2, "async 2 no error")
    assert.is_nil(err3, "async 3 no error")

    -- Wait for all
    local r1 = f1:await()
    local r2 = f2:await()
    local r3 = f3:await()

    assert.eq(r1.completed, true, "r1 completed")
    assert.eq(r2.completed, true, "r2 completed")
    assert.eq(r3.completed, true, "r3 completed")

    -- Total time should be ~50ms (parallel), not ~150ms (sequential)
    local elapsed = time.now():sub(start)
    local elapsed_ms = elapsed:milliseconds()
    assert.ok(elapsed_ms < 200, "parallel execution (< 200ms): " .. elapsed_ms .. "ms")

    return true
end

return { main = main }
