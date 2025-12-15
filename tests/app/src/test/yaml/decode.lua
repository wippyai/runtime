-- Test: yaml.decode function
local assert = require("assert_primitives")

local function main()
    local yaml = require("yaml")

    -- Decode object
    local result = yaml.decode("name: test\nvalue: 123")
    assert.not_nil(result, "decode object returns result")
    assert.eq(result.name, "test", "name field correct")
    assert.eq(result.value, 123, "value field correct")

    -- Decode array
    local arr = yaml.decode("- 1\n- 2\n- 3")
    assert.not_nil(arr, "decode array returns result")
    assert.eq(arr[1], 1, "first element correct")
    assert.eq(arr[2], 2, "second element correct")
    assert.eq(arr[3], 3, "third element correct")

    -- Decode nested
    local nested = yaml.decode([[
parent:
  child:
    value: 123
]])
    assert.not_nil(nested, "decode nested returns result")
    assert.eq(nested.parent.child.value, 123, "nested value correct")

    -- Decode with mixed types
    local mixed = yaml.decode([[
str: hello
num: 42
bool: true
arr:
  - 1
  - 2
]])
    assert.eq(mixed.str, "hello", "string field correct")
    assert.eq(mixed.num, 42, "number field correct")
    assert.eq(mixed.bool, true, "boolean field correct")
    assert.eq(mixed.arr[1], 1, "array element correct")

    -- Invalid input type error
    local _, err1 = yaml.decode(123)
    assert.not_nil(err1, "non-string input should error")
    assert.eq(err1:kind(), errors.INVALID, "invalid input error kind")
    assert.eq(err1:retryable(), false, "invalid input not retryable")

    -- Empty string error
    local _, err2 = yaml.decode("")
    assert.not_nil(err2, "empty string should error")
    assert.eq(err2:kind(), errors.INVALID, "empty string error kind")

    -- Invalid YAML error
    local _, err3 = yaml.decode(":\n  :\n  invalid")
    assert.not_nil(err3, "invalid yaml should error")
    assert.eq(err3:kind(), errors.INTERNAL, "invalid yaml error kind")
    assert.eq(err3:retryable(), false, "invalid yaml not retryable")

    return true
end

return { main = main }
