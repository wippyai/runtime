-- SPDX-License-Identifier: MPL-2.0

-- Test: hash.hmac_* functions
local assert = require("assert2")
local hash = require("hash")

local function main()
-- HMAC-MD5 tests
	local result = hash.hmac_md5("hello", "secret")
	assert.eq(#result, 32, "hmac_md5 is 32 hex chars")

	result = hash.hmac_md5("hello", "secret", true)
	assert.eq(#result, 16, "hmac_md5 raw is 16 bytes")

	-- HMAC-SHA1 tests
	result = hash.hmac_sha1("hello", "secret")
	assert.eq(#result, 40, "hmac_sha1 is 40 hex chars")

	result = hash.hmac_sha1("hello", "secret", true)
	assert.eq(#result, 20, "hmac_sha1 raw is 20 bytes")

	-- HMAC-SHA256 tests
	result = hash.hmac_sha256("hello", "secret")
	assert.eq(#result, 64, "hmac_sha256 is 64 hex chars")

	result = hash.hmac_sha256("hello", "secret", true)
	assert.eq(#result, 32, "hmac_sha256 raw is 32 bytes")

	-- HMAC-SHA512 tests
	result = hash.hmac_sha512("hello", "secret")
	assert.eq(#result, 128, "hmac_sha512 is 128 hex chars")

	result = hash.hmac_sha512("hello", "secret", true)
	assert.eq(#result, 64, "hmac_sha512 raw is 64 bytes")

	-- Determinism
	local h1 = hash.hmac_sha256("test", "key")
	local h2 = hash.hmac_sha256("test", "key")
	assert.eq(h1, h2, "hmac_sha256 deterministic")

	-- Different keys produce different results
	h1 = hash.hmac_sha256("hello", "key1")
	h2 = hash.hmac_sha256("hello", "key2")
	assert.neq(h1, h2, "hmac_sha256 different for different keys")

	return true
end

return { main = main }
