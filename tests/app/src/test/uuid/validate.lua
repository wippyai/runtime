-- Test: UUID validation
local assert = require("assert2")
local uuid = require("uuid")

local function main()
    -- Test valid UUIDs
    local valid1 = uuid.validate("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
    assert.eq(valid1, true, "standard UUID valid")

    local valid2 = uuid.validate("550e8400-e29b-41d4-a716-446655440000")
    assert.eq(valid2, true, "another UUID valid")

    -- Test generated UUIDs are valid
    local v4, _ = uuid.v4()
    local valid3 = uuid.validate(v4)
    assert.eq(valid3, true, "generated v4 valid")

    -- Test invalid UUIDs
    local invalid1 = uuid.validate("not-a-uuid")
    assert.eq(invalid1, false, "invalid string")

    local invalid2 = uuid.validate("")
    assert.eq(invalid2, false, "empty string")

    local invalid3 = uuid.validate("6ba7b810-9dad-11d1-80b4")
    assert.eq(invalid3, false, "truncated UUID")

    local invalid4 = uuid.validate("6ba7b810-9dad-11d1-80b4-00c04fd430c8-extra")
    assert.eq(invalid4, false, "UUID with extra chars")

    -- Test non-string input (use any to test runtime validation)
    local num_input: any = 123
    local invalid5 = uuid.validate(num_input :: string)
    assert.eq(invalid5, false, "number input")

    local nil_input: any = nil
    local invalid6 = uuid.validate(nil_input :: string)
    assert.eq(invalid6, false, "nil input")

    local table_input: any = {}
    local invalid7 = uuid.validate(table_input :: string)
    assert.eq(invalid7, false, "table input")

    return true
end

return { main = main }
