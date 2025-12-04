local assert = require("assert_primitives")

local function main()
    -- test get by variable alias (app.test.env:test_var -> TEST_VAR)
    local val, err = env.get("app.test.env:test_var")
    assert.eq(err, nil, "get by alias should not return error")
    assert.eq(val, "test_value", "should get value by alias")

    -- test get by direct variable ID (ANOTHER_VAR from memory storage)
    val, err = env.get("app.test.env:composite/ANOTHER_VAR")
    assert.eq(err, nil, "get by ID should not return error")
    assert.eq(val, "another_value", "should get value by ID")

    -- test get numeric value (stored as string)
    val, err = env.get("app.test.env:composite/NUMERIC_VAR")
    assert.eq(err, nil, "get numeric should not return error")
    assert.eq(val, "12345", "numeric values are strings")

    -- test get OS variable via os storage
    val, err = env.get("app.test.env:os/PATH")
    assert.eq(err, nil, "get OS PATH should not return error")
    assert.ok(val ~= nil and #val > 0, "PATH should not be empty")

    -- test get via path_var alias (references OS PATH)
    val, err = env.get("app.test.env:path_var")
    assert.eq(err, nil, "get PATH by alias should not return error")
    assert.ok(val ~= nil and #val > 0, "PATH via alias should not be empty")

    -- test fallback: memory first, then OS
    local memory_val, _ = env.get("app.test.env:memory/TEST_VAR")
    local composite_val, _ = env.get("app.test.env:composite/TEST_VAR")
    assert.eq(memory_val, composite_val, "composite should find value from memory storage")
end

return { main = main }
