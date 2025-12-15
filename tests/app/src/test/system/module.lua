local assert = require("assert_primitives")

local function main()
    local system = require("system")

    assert.not_nil(system, "system module loaded")
    assert.eq(type(system), "table", "system is table")

    -- Check child tables exist
    assert.not_nil(system.memory, "memory table exists")
    assert.eq(type(system.memory), "table", "memory is table")

    assert.not_nil(system.gc, "gc table exists")
    assert.eq(type(system.gc), "table", "gc is table")

    assert.not_nil(system.runtime, "runtime table exists")
    assert.eq(type(system.runtime), "table", "runtime is table")

    assert.not_nil(system.process, "process table exists")
    assert.eq(type(system.process), "table", "process is table")

    assert.not_nil(system.supervisor, "supervisor table exists")
    assert.eq(type(system.supervisor), "table", "supervisor is table")

    -- Check top-level functions
    assert.eq(type(system.exit), "function", "exit is function")
    assert.eq(type(system.modules), "function", "modules is function")

    -- Check memory functions
    assert.eq(type(system.memory.stats), "function", "memory.stats is function")
    assert.eq(type(system.memory.allocated), "function", "memory.allocated is function")
    assert.eq(type(system.memory.heap_objects), "function", "memory.heap_objects is function")
    assert.eq(type(system.memory.set_limit), "function", "memory.set_limit is function")
    assert.eq(type(system.memory.get_limit), "function", "memory.get_limit is function")

    -- Check gc functions
    assert.eq(type(system.gc.collect), "function", "gc.collect is function")
    assert.eq(type(system.gc.set_percent), "function", "gc.set_percent is function")
    assert.eq(type(system.gc.get_percent), "function", "gc.get_percent is function")

    -- Check runtime functions
    assert.eq(type(system.runtime.goroutines), "function", "runtime.goroutines is function")
    assert.eq(type(system.runtime.max_procs), "function", "runtime.max_procs is function")
    assert.eq(type(system.runtime.cpu_count), "function", "runtime.cpu_count is function")

    -- Check process functions
    assert.eq(type(system.process.pid), "function", "process.pid is function")
    assert.eq(type(system.process.hostname), "function", "process.hostname is function")

    -- Check supervisor functions
    assert.eq(type(system.supervisor.state), "function", "supervisor.state is function")
    assert.eq(type(system.supervisor.states), "function", "supervisor.states is function")

    return true
end

return { main = main }
