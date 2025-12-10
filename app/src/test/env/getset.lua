local assert = require("assert_primitives")
local env = require("env")

return function()
    -- Test setting a new variable
    local ok, err = env.set("TEST_VAR_123", "hello_world")
    assert.is_nil(err, "env.set should not return error")
    assert.eq(ok, true, "env.set should return true on success")

    -- Test getting the set variable
    local val, err = env.get("TEST_VAR_123")
    assert.is_nil(err, "env.get should not return error")
    assert.eq(val, "hello_world", "env.get should return the set value")

    -- Test overwriting a variable
    local ok, err = env.set("TEST_VAR_123", "new_value")
    assert.is_nil(err, "env.set should not return error on overwrite")
    assert.eq(ok, true, "env.set should return true on overwrite")

    local val, err = env.get("TEST_VAR_123")
    assert.is_nil(err, "env.get should not return error")
    assert.eq(val, "new_value", "env.get should return the overwritten value")

end
