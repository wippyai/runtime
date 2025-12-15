local assert = require("assert_primitives")

local function main()
    local system = require("system")

    -- Test memory.stats
    local stats, err = system.memory.stats()
    assert.is_nil(err, "stats should not error")
    assert.not_nil(stats, "stats returned")
    assert.eq(type(stats), "table", "stats is table")
    assert.ok(stats.alloc > 0, "alloc > 0")
    assert.ok(stats.heap_objects > 0, "heap_objects > 0")
    assert.not_nil(stats.num_gc, "num_gc exists")
    assert.not_nil(stats.total_alloc, "total_alloc exists")
    assert.not_nil(stats.sys, "sys exists")
    assert.not_nil(stats.heap_alloc, "heap_alloc exists")
    assert.not_nil(stats.heap_sys, "heap_sys exists")

    -- Test memory.allocated
    local alloc, err = system.memory.allocated()
    assert.is_nil(err, "allocated should not error")
    assert.not_nil(alloc, "allocated returned")
    assert.eq(type(alloc), "number", "allocated is number")
    assert.ok(alloc > 0, "allocated > 0")

    -- Test memory.heap_objects
    local objs, err = system.memory.heap_objects()
    assert.is_nil(err, "heap_objects should not error")
    assert.not_nil(objs, "heap_objects returned")
    assert.eq(type(objs), "number", "heap_objects is number")
    assert.ok(objs > 0, "heap_objects > 0")

    -- Test memory.get_limit
    local limit, err = system.memory.get_limit()
    assert.is_nil(err, "get_limit should not error")
    assert.not_nil(limit, "get_limit returned")
    assert.eq(type(limit), "number", "get_limit is number")

    return true
end

return { main = main }
