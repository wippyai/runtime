local assert = require("assert_primitives")

local function main()
    local system = require("system")

    -- Test gc.collect
    local ok, err = system.gc.collect()
    assert.is_nil(err, "collect should not error")
    assert.eq(ok, true, "collect returns true")

    -- Test gc.get_percent
    local orig, err = system.gc.get_percent()
    assert.is_nil(err, "get_percent should not error")
    assert.not_nil(orig, "get_percent returned")
    assert.eq(type(orig), "number", "get_percent is number")

    -- Test gc.set_percent
    local old, err = system.gc.set_percent(200)
    assert.is_nil(err, "set_percent should not error")
    assert.not_nil(old, "set_percent returned old value")
    assert.eq(type(old), "number", "set_percent returns number")

    -- Verify new value
    local new, err = system.gc.get_percent()
    assert.is_nil(err, "get_percent after set should not error")
    assert.eq(new, 200, "gc percent changed to 200")

    -- Restore original value
    system.gc.set_percent(orig)

    return true
end

return { main = main }
