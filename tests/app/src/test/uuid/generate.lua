-- Test: UUID generation functions
local assert = require("assert2")
local uuid = require("uuid")

local function main()
    -- Test uuid.v1() generates valid UUID
    local v1, err = uuid.v1()
    assert.is_nil(err, "v1 no error")
    assert.not_nil(v1, "v1 generated")
    assert.eq(#v1, 36, "v1 length is 36")
    assert.eq(uuid.validate(v1), true, "v1 is valid")

    -- Test uuid.v4() generates valid UUID
    local v4, err = uuid.v4()
    assert.is_nil(err, "v4 no error")
    assert.not_nil(v4, "v4 generated")
    assert.eq(#v4, 36, "v4 length is 36")
    assert.eq(uuid.validate(v4), true, "v4 is valid")

    -- Test uuid.v7() generates valid UUID
    local v7, err = uuid.v7()
    assert.is_nil(err, "v7 no error")
    assert.not_nil(v7, "v7 generated")
    assert.eq(#v7, 36, "v7 length is 36")
    assert.eq(uuid.validate(v7), true, "v7 is valid")

    -- Test uuid.v3() with namespace and name
    local ns = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
    local v3, err = uuid.v3(ns, "test")
    assert.is_nil(err, "v3 no error")
    assert.not_nil(v3, "v3 generated")
    assert.eq(#v3, 36, "v3 length is 36")
    assert.eq(uuid.validate(v3), true, "v3 is valid")

    -- Test uuid.v5() with namespace and name
    local v5, err = uuid.v5(ns, "test")
    assert.is_nil(err, "v5 no error")
    assert.not_nil(v5, "v5 generated")
    assert.eq(#v5, 36, "v5 length is 36")
    assert.eq(uuid.validate(v5), true, "v5 is valid")

    -- Test v3/v5 are deterministic
    local v3_again, _ = uuid.v3(ns, "test")
    assert.eq(v3, v3_again, "v3 is deterministic")

    local v5_again, _ = uuid.v5(ns, "test")
    assert.eq(v5, v5_again, "v5 is deterministic")

    -- Test v3 and v5 produce different results
    assert.neq(v3, v5, "v3 and v5 differ")

    -- Test v4 uniqueness
    local seen = {}
    for i = 1, 50 do
        local id, _ = uuid.v4()
        assert.is_nil(seen[id], "v4 unique " .. i)
        seen[id] = true
    end

    return true
end

return { main = main }
