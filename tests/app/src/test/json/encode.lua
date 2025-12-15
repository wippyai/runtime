-- Test: json.encode function
local assert = require("assert_primitives")

local function main()
    local json = require("json")

    -- Encode nil
    local result = json.encode(nil)
    assert.eq(result, "null", "nil encodes to null")

    -- Encode boolean
    assert.eq(json.encode(true), "true", "true encodes correctly")
    assert.eq(json.encode(false), "false", "false encodes correctly")

    -- Encode number
    assert.eq(json.encode(42), "42", "integer encodes correctly")
    assert.eq(json.encode(3.14), "3.14", "float encodes correctly")

    -- Encode string
    assert.eq(json.encode("hello"), '"hello"', "string encodes correctly")
    assert.eq(json.encode(""), '""', "empty string encodes correctly")

    -- Encode array
    local arr = json.encode({1, 2, 3})
    assert.eq(arr, "[1,2,3]", "array encodes correctly")

    -- Encode object
    local obj, err = json.encode({name = "test"})
    assert.is_nil(err, "object encode should not error")
    assert.contains(obj, '"name"', "object contains key")
    assert.contains(obj, '"test"', "object contains value")

    -- Encode nested
    local nested, err2 = json.encode({items = {1, 2}, meta = {ok = true}})
    assert.is_nil(err2, "nested encode should not error")
    assert.not_nil(nested, "nested returns result")

    -- Encode empty table as array
    local empty = json.encode({})
    assert.eq(empty, "[]", "empty table encodes as array")

    -- Sparse array error
    local sparse = {[1] = "a", [3] = "c"}
    local _, sparse_err = json.encode(sparse)
    assert.not_nil(sparse_err, "sparse array should error")
    assert.eq(sparse_err:kind(), errors.INTERNAL, "sparse array error kind")
    assert.eq(sparse_err:retryable(), false, "sparse array not retryable")

    return true
end

return { main = main }
