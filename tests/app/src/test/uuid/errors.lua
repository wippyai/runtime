-- Test: UUID error handling
local assert = require("assert2")
local uuid = require("uuid")

local function main()
    -- Test v3 with invalid namespace
    local result, err = uuid.v3("invalid", "test")
    assert.is_nil(result, "v3 invalid ns nil result")
    assert.not_nil(err, "v3 invalid ns has error")
    assert.eq(err:kind(), errors.INVALID, "v3 invalid ns kind")
    assert.eq(err:retryable(), false, "v3 invalid ns not retryable")

    -- Test v5 with invalid namespace
    local result3, err3 = uuid.v5("not-uuid", "name")
    assert.is_nil(result3, "v5 invalid ns nil result")
    assert.not_nil(err3, "v5 invalid ns has error")
    assert.eq(err3:kind(), errors.INVALID, "v5 invalid ns kind")

    -- Test version with invalid input
    local result4, err4 = uuid.version("invalid")
    assert.is_nil(result4, "version invalid nil result")
    assert.not_nil(err4, "version invalid has error")
    assert.eq(err4:kind(), errors.INVALID, "version invalid kind")

    -- Test version with non-string (use any to test runtime error)
    local bad_input: any = 123
    local result5, err5 = uuid.version(bad_input)
    assert.is_nil(result5, "version number nil result")
    assert.not_nil(err5, "version number has error")
    assert.eq(err5:kind(), errors.INVALID, "version number kind")

    -- Test variant with invalid input
    local result6, err6 = uuid.variant("invalid")
    assert.is_nil(result6, "variant invalid nil result")
    assert.not_nil(err6, "variant invalid has error")
    assert.eq(err6:kind(), errors.INVALID, "variant invalid kind")

    -- Test parse with invalid input
    local result7, err7 = uuid.parse("not-a-uuid")
    assert.is_nil(result7, "parse invalid nil result")
    assert.not_nil(err7, "parse invalid has error")
    assert.eq(err7:kind(), errors.INVALID, "parse invalid kind")

    -- Test format with invalid UUID
    local result8, err8 = uuid.format("invalid")
    assert.is_nil(result8, "format invalid uuid nil result")
    assert.not_nil(err8, "format invalid uuid has error")
    assert.eq(err8:kind(), errors.INVALID, "format invalid uuid kind")

    -- Test format with unsupported format type
    local result9, err9 = uuid.format("6ba7b810-9dad-11d1-80b4-00c04fd430c8", "unknown")
    assert.is_nil(result9, "format unknown type nil result")
    assert.not_nil(err9, "format unknown type has error")
    assert.eq(err9:kind(), errors.INVALID, "format unknown type kind")

    -- Verify errors have string representation
    local str = tostring(err)
    assert.not_nil(str, "error has tostring")
    assert.neq(str, "", "error string not empty")

    return true
end

return { main = main }
