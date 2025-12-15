-- Test: payload format constants
local assert = require("assert2")

local function main()
    -- Test format constants exist
    assert.not_nil(payload.format, "format table exists")

    -- Test format constant values
    assert.eq(payload.format.JSON, "json/plain", "JSON format")
    assert.eq(payload.format.YAML, "yaml/plain", "YAML format")
    assert.eq(payload.format.STRING, "text/plain", "STRING format")
    assert.eq(payload.format.BYTES, "application/octet-stream", "BYTES format")
    assert.eq(payload.format.MSGPACK, "application/msgpack", "MSGPACK format")
    assert.eq(payload.format.LUA, "lua/any", "LUA format")
    assert.eq(payload.format.GOLANG, "golang/any", "GOLANG format")
    assert.eq(payload.format.ERROR, "golang/error", "ERROR format")

    -- Test get_format method
    local p = payload.new({test = true})
    assert.eq(p:get_format(), payload.format.LUA, "get_format returns LUA")

    return true
end

return { main = main }
