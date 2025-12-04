-- Test: Parallel async execution
local assert = require("assert2")
local funcs = require("funcs")
local time = require("time")

local function main()
    local start = time.now()

    -- Start 5 slow operations in parallel
    local futures = {}
    for i = 1, 5 do
        futures[i] = funcs.async("app.test.funcs:slow", 50, "task-" .. i)
    end

    -- All should not be complete immediately
    for i, f in ipairs(futures) do
        assert.eq(f:is_complete(), false, "future " .. i .. " not complete immediately")
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
        assert.eq(r.value, "task-" .. i, "result " .. i .. " has correct value")
    end

    -- Parallel execution should be faster than sequential (5*50=250ms)
    -- Allow some overhead but should be well under 200ms for parallel
    assert.ok(elapsed_ms < 200, "parallel execution faster than sequential: " .. elapsed_ms .. "ms")

    return true
end

return { main = main }
