-- Test: json.validate and json.validate_string functions
local assert = require("assert_primitives")

local function main()
    local json = require("json")

    -- Basic validation success
    local schema = {
        type = "object",
        properties = {
            name = {type = "string"},
            age = {type = "number"}
        },
        required = {"name"}
    }

    local valid, err = json.validate(schema, {name = "John", age = 30})
    assert.is_nil(err, "valid data should not error")
    assert.eq(valid, true, "valid data returns true")

    -- Validation failure - missing required field
    local valid2, err2 = json.validate(schema, {age = 30})
    assert.eq(valid2, false, "invalid data returns false")
    assert.not_nil(err2, "invalid data returns error")
    assert.eq(err2:kind(), errors.INVALID, "validation error kind")
    assert.eq(err2:retryable(), false, "validation error not retryable")

    -- Validation failure - wrong type
    local valid3, err3 = json.validate(schema, {name = 123})
    assert.eq(valid3, false, "wrong type returns false")
    assert.not_nil(err3, "wrong type returns error")
    assert.eq(err3:kind(), errors.INVALID, "wrong type error kind")

    -- Schema as JSON string
    local schema_str = '{"type":"object","properties":{"name":{"type":"string"}}}'
    local valid4, err4 = json.validate(schema_str, {name = "test"})
    assert.is_nil(err4, "string schema should work")
    assert.eq(valid4, true, "string schema validates")

    -- Missing schema error
    local valid5, err5 = json.validate(nil, {name = "test"})
    assert.eq(valid5, false, "missing schema returns false")
    assert.not_nil(err5, "missing schema returns error")
    assert.eq(err5:kind(), errors.INVALID, "missing schema error kind")
    assert.eq(err5:retryable(), false, "missing schema not retryable")
    local str5 = tostring(err5)
    assert.contains(str5, "schema is required", "missing schema error message")

    -- Missing data error
    local valid6, err6 = json.validate(schema, nil)
    assert.eq(valid6, false, "missing data returns false")
    assert.not_nil(err6, "missing data returns error")
    assert.eq(err6:kind(), errors.INVALID, "missing data error kind")
    local str6 = tostring(err6)
    assert.contains(str6, "data is required", "missing data error message")

    -- validate_string success
    local valid7, err7 = json.validate_string(schema, '{"name":"Jane","age":25}')
    assert.is_nil(err7, "validate_string should not error")
    assert.eq(valid7, true, "validate_string returns true for valid")

    -- validate_string failure
    local valid8, err8 = json.validate_string(schema, '{"age":25}')
    assert.eq(valid8, false, "validate_string returns false for invalid")
    assert.not_nil(err8, "validate_string returns error for invalid")
    assert.eq(err8:kind(), errors.INVALID, "validate_string error kind")

    -- validate_string non-string data error
    local valid9, err9 = json.validate_string(schema, 123)
    assert.eq(valid9, false, "validate_string with non-string returns false")
    assert.not_nil(err9, "validate_string with non-string returns error")
    assert.eq(err9:kind(), errors.INVALID, "validate_string non-string error kind")
    local str9 = tostring(err9)
    assert.contains(str9, "JSON string", "validate_string non-string error message")

    return true
end

return { main = main }
