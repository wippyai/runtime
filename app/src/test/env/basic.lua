local assert = require("assert_primitives")
local env = require("env")

return function()
    -- Test env.get returns value, err
    local val, err = env.get("PATH")
    assert.is_nil(err, "env.get should not return error for PATH")
    assert.not_nil(val, "PATH should exist in environment")

    -- Test env.get_all returns table
    local all, err = env.get_all()
    assert.is_nil(err, "env.get_all should not return error")
    assert.eq(type(all), "table", "env.get_all should return table")

    -- PATH should be in the result
    assert.not_nil(all.PATH, "PATH should exist in env.get_all result")
end
