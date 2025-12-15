-- Test: hash.md5 function
local assert = require("assert2")
local hash = require("hash")

local function main()
    -- Test correct hash for "hello"
    local result = hash.md5("hello")
    assert.eq(result, "5d41402abc4b2a76b9719d911017c592", "md5 hello")

    -- Test correct hash for empty string
    result = hash.md5("")
    assert.eq(result, "d41d8cd98f00b204e9800998ecf8427e", "md5 empty")

    -- Test raw bytes output
    result = hash.md5("hello", true)
    assert.eq(#result, 16, "md5 raw is 16 bytes")

    -- Test determinism
    local h1 = hash.md5("test")
    local h2 = hash.md5("test")
    assert.eq(h1, h2, "md5 deterministic")

    -- Test different inputs produce different hashes
    h1 = hash.md5("hello")
    h2 = hash.md5("world")
    assert.neq(h1, h2, "md5 different for different inputs")

    return true
end

return { main = main }
