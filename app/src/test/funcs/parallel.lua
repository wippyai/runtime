-- Test: Parallel async execution
local assert = require("assert2")
local funcs = require("funcs")
local time = require("time")

local function main()
    local start = time.now()

    -- Start 5 slow operations in parallel (100ms each)
    local futures = {}
    for i = 1, 5 do
        futures[i] = funcs.async("app.test.funcs:slow", 100, "task-" .. i)
    end

    -- Await all
    local results = {}
    for i, f in ipairs(futures) do
        results[i] = f:await()
    end

    local elapsed = time.now():sub(start)
    local elapsed_ms = elapsed:milliseconds()

    -- All should have results
    for i, r in ipairs(results) do
        assert.not_nil(r, "result " .. i .. " not nil")
        assert.eq(r:get("value"), "task-" .. i, "result " .. i .. " has correct value")
    end

    -- Parallel execution should be faster than sequential (5*100=500ms)
    -- Allow some overhead but should be well under 400ms for parallel
    assert.ok(elapsed_ms < 400, "parallel execution faster than sequential: " .. elapsed_ms .. "ms")

    return true
end

return { main = main }
