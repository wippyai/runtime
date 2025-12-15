-- Test: Basic process registry operations
local assert = require("assert2")
local time = require("time")

local function main()
    local test_name = "test_registry_" .. tostring(os.time())

    -- Register self with a name
    local ok, err = process.registry.register(test_name)
    if err then
        return false, "register failed: " .. tostring(err)
    end

    -- Lookup the registered name
    local pid, lookup_err = process.registry.lookup(test_name)
    assert.is_nil(lookup_err, "lookup no error")
    assert.eq(pid, process.pid(), "lookup returns self pid")

    -- Register same name again overwrites (this is expected behavior)
    ok, err = process.registry.register(test_name)
    if err then
        return false, "re-register failed: " .. tostring(err)
    end

    -- Verify still resolves to self
    pid, lookup_err = process.registry.lookup(test_name)
    assert.is_nil(lookup_err, "lookup after re-register no error")
    assert.eq(pid, process.pid(), "lookup after re-register returns self pid")

    -- Unregister
    local unregistered = process.registry.unregister(test_name)
    assert.ok(unregistered, "unregister succeeded")

    -- Lookup after unregister should fail
    pid, err = process.registry.lookup(test_name)
    assert.is_nil(pid, "lookup after unregister returns nil")
    assert.not_nil(err, "lookup after unregister has error")

    -- Unregister again should return false
    unregistered = process.registry.unregister(test_name)
    assert.eq(unregistered, false, "double unregister returns false")

    return true
end

return { main = main }
