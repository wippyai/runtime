local assert = require("assert_primitives")

local function main()
    -- test get_all from memory storage
    local vars, err = env.get_all("app.test.env:memory")
    assert.eq(err, nil, "get_all should not return error")
    assert.ok(type(vars) == "table", "should return table")
    assert.eq(vars.TEST_VAR, "test_value", "should contain TEST_VAR")
    assert.eq(vars.ANOTHER_VAR, "another_value", "should contain ANOTHER_VAR")
    assert.eq(vars.NUMERIC_VAR, "12345", "should contain NUMERIC_VAR")

    -- test get_all from OS storage (should have system vars)
    vars, err = env.get_all("app.test.env:os")
    assert.eq(err, nil, "get_all OS should not return error")
    assert.ok(type(vars) == "table", "should return table for OS")
    assert.ok(vars.PATH ~= nil, "OS should contain PATH")

    -- test get_all from composite (should merge memory + OS)
    vars, err = env.get_all("app.test.env:composite")
    assert.eq(err, nil, "get_all composite should not return error")
    assert.ok(type(vars) == "table", "should return table for composite")
    assert.eq(vars.TEST_VAR, "test_value", "composite should have memory vars")
    assert.ok(vars.PATH ~= nil, "composite should have OS vars")
end

return { main = main }
