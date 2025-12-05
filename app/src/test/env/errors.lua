local assert = require("assert_primitives")
local env = require("env")

return function()
    -- Debug: verify env.set is a function
    print("env.set type:", type(env.set))

    -- Test getting non-existent variable returns error
    local val, err = env.get("NONEXISTENT_VAR_XYZ_123")
    assert.not_nil(err, "env.get with non-existent var should return error")
    assert.is_nil(val, "env.get should return nil for non-existent var")

    -- Test empty key argument throws (l.RaiseError causes error)
    local ok, err = pcall(function()
        env.get("")
    end)
    print("env.get('') result: ok=", ok, "err=", err)
    assert.eq(ok, false, "env.get with empty key should error")

    -- Test empty key for set throws (l.RaiseError causes error)
    print("About to test env.set('')...")
    local ok2, err2 = pcall(function()
        print("Inside pcall, calling env.set('')...")
        local result = env.set("", "value")
        print("env.set returned:", result)
        return result
    end)
    print("env.set('') result: ok2=", ok2, "err2=", err2)
    assert.eq(ok2, false, "env.set with empty key should error")
end
