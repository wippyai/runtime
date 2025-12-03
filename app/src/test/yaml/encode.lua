-- Test: yaml.encode function
local assert = require("assert_primitives")

local function main()
    local yaml = require("yaml")

    -- Encode table
    local result, err = yaml.encode({name = "test", value = 123})
    assert.is_nil(err, "encode table should not error")
    assert.not_nil(result, "encode table returns result")
    assert.contains(result, "name", "result contains name key")
    assert.contains(result, "test", "result contains test value")

    -- Encode array
    local arr, err2 = yaml.encode({1, 2, 3})
    assert.is_nil(err2, "encode array should not error")
    assert.not_nil(arr, "encode array returns result")

    -- Encode nested
    local nested, err3 = yaml.encode({parent = {child = {value = 42}}})
    assert.is_nil(err3, "encode nested should not error")
    assert.contains(nested, "parent", "nested contains parent")
    assert.contains(nested, "child", "nested contains child")

    -- Encode with mixed types
    local mixed, err4 = yaml.encode({
        str = "hello",
        num = 42,
        bool = true,
        arr = {1, 2, 3}
    })
    assert.is_nil(err4, "encode mixed types should not error")
    assert.not_nil(mixed, "encode mixed types returns result")

    -- Invalid input type error
    local _, err5 = yaml.encode(123)
    assert.not_nil(err5, "non-table input should error")
    assert.eq(err5:kind(), errors.INVALID, "invalid input error kind")
    assert.eq(err5:retryable(), false, "invalid input not retryable")

    -- Invalid input type (string)
    local _, err6 = yaml.encode("not a table")
    assert.not_nil(err6, "string input should error")
    assert.eq(err6:kind(), errors.INVALID, "string input error kind")

    -- Missing input error
    local _, err7 = yaml.encode()
    assert.not_nil(err7, "missing input should error")
    assert.eq(err7:kind(), errors.INVALID, "missing input error kind")

    return true
end

return { main = main }
