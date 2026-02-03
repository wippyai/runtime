-- Test: hash.sha1/sha256/sha512 functions
local assert = require("assert2")
local hash = require("hash")

local function main()
-- SHA1 tests
	local result = hash.sha1("hello")
	assert.eq(result, "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d", "sha1 hello")
	assert.eq(#result, 40, "sha1 is 40 hex chars")

	result = hash.sha1("hello", true)
	assert.eq(#result, 20, "sha1 raw is 20 bytes")

	-- SHA256 tests
	result = hash.sha256("hello")
	assert.eq(result, "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824", "sha256 hello")
	assert.eq(#result, 64, "sha256 is 64 hex chars")

	result = hash.sha256("hello", true)
	assert.eq(#result, 32, "sha256 raw is 32 bytes")

	-- SHA512 tests
	result = hash.sha512("hello")
	assert.eq(#result, 128, "sha512 is 128 hex chars")

	result = hash.sha512("hello", true)
	assert.eq(#result, 64, "sha512 raw is 64 bytes")

	-- Determinism
	local h1 = hash.sha256("test")
	local h2 = hash.sha256("test")
	assert.eq(h1, h2, "sha256 deterministic")

	return true
end

return { main = main }
