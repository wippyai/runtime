-- Test: json.decode function
local assert = require("assert_primitives")

local function main()
    local json = require("json")

    -- Decode null
    local result = json.decode("null")
    assert.is_nil(result, "null decodes to nil")

    -- Decode boolean
    assert.eq(json.decode("true"), true, "true decodes correctly")
    assert.eq(json.decode("false"), false, "false decodes correctly")

    -- Decode number
    assert.eq(json.decode("42"), 42, "integer decodes correctly")
    assert.eq(json.decode("3.14"), 3.14, "float decodes correctly")

    -- Decode string
    assert.eq(json.decode('"hello"'), "hello", "string decodes correctly")
    assert.eq(json.decode('""'), "", "empty string decodes correctly")

    -- Decode array
    local arr = json.decode("[1, 2, 3]")
    assert.eq(arr[1], 1, "array first element")
    assert.eq(arr[2], 2, "array second element")
    assert.eq(arr[3], 3, "array third element")

    -- Decode object
    local obj = json.decode('{"name":"test","value":123}')
    assert.eq(obj.name, "test", "object string field")
    assert.eq(obj.value, 123, "object number field")

    -- Decode nested
    local nested = json.decode('{"items":[1,2],"meta":{"ok":true}}')
    assert.eq(nested.items[1], 1, "nested array element")
    assert.eq(nested.meta.ok, true, "nested object field")

    -- Invalid input type error
    local _, err1 = json.decode(123)
    assert.not_nil(err1, "non-string input should error")
    assert.eq(err1:kind(), errors.INVALID, "invalid input error kind")
    assert.eq(err1:retryable(), false, "invalid input not retryable")
    local str1 = tostring(err1)
    assert.contains(str1, "string expected", "error message for invalid input")

    -- Empty string error
    local _, err2 = json.decode("")
    assert.not_nil(err2, "empty string should error")
    assert.eq(err2:kind(), errors.INVALID, "empty string error kind")
    assert.eq(err2:retryable(), false, "empty string not retryable")

    -- Invalid JSON error
    local _, err3 = json.decode("not json")
    assert.not_nil(err3, "invalid json should error")
    assert.eq(err3:kind(), errors.INTERNAL, "invalid json error kind")
    assert.eq(err3:retryable(), false, "invalid json not retryable")

    return true
end

return { main = main }
