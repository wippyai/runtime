-- Test: UUID formatting
local assert = require("assert2")
local uuid = require("uuid")

local function main()
    local id = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"

    -- Test standard format
    local std, err = uuid.format(id, "standard")
    assert.is_nil(err, "standard no error")
    assert.eq(std, id, "standard unchanged")

    -- Test simple format (no dashes)
    local simple, err = uuid.format(id, "simple")
    assert.is_nil(err, "simple no error")
    assert.eq(simple, "6ba7b8109dad11d180b400c04fd430c8", "simple no dashes")
    assert.eq(#simple, 32, "simple length 32")

    -- Test urn format
    local urn, err = uuid.format(id, "urn")
    assert.is_nil(err, "urn no error")
    assert.eq(urn, "urn:uuid:" .. id, "urn prefix")

    -- Test default format (standard)
    local default, err = uuid.format(id)
    assert.is_nil(err, "default no error")
    assert.eq(default, id, "default is standard")

    -- Test format on generated UUID
    local v4, _ = uuid.v4()
    local v4_simple, _ = uuid.format(v4, "simple")
    assert.eq(#v4_simple, 32, "v4 simple length")

    local v4_urn, _ = uuid.format(v4, "urn")
    assert.eq(v4_urn, "urn:uuid:" .. v4, "v4 urn prefix")

    return true
end

return { main = main }
