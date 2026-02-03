-- Test: crypto.pbkdf2 and constant_time_compare
local assert = require("assert2")

local function main()
	local crypto = require("crypto")

	-- pbkdf2 basic
	local key, err = crypto.pbkdf2("password", "salt", 1000, 32)
	assert.is_nil(err, "pbkdf2 should not error")
	assert.not_nil(key, "pbkdf2 returns key")
	assert.eq(#key, 32, "pbkdf2 returns correct length")

	-- Same inputs produce same output
	local key2, _ = crypto.pbkdf2("password", "salt", 1000, 32)
	assert.eq(key, key2, "pbkdf2 is deterministic")

	-- Different password produces different key
	local key3, _ = crypto.pbkdf2("other", "salt", 1000, 32)
	assert.neq(key, key3, "different password produces different key")

	-- Different salt produces different key
	local key4, _ = crypto.pbkdf2("password", "other", 1000, 32)
	assert.neq(key, key4, "different salt produces different key")

	-- sha512 hash function
	local key5, err2 = crypto.pbkdf2("password", "salt", 1000, 64, "sha512")
	assert.is_nil(err2, "pbkdf2 with sha512 should not error")
	assert.eq(#key5, 64, "sha512 returns correct length")

	-- Invalid hash function
	local _, err3 = crypto.pbkdf2("password", "salt", 1000, 32, "md5")
	assert.not_nil(err3, "invalid hash function should error")
	assert.eq(err3:kind(), errors.INVALID, "invalid hash error kind")
	assert.eq(err3:retryable(), false, "invalid hash not retryable")

	-- Empty password should error
	local _, err4 = crypto.pbkdf2("", "salt", 1000, 32)
	assert.not_nil(err4, "empty password should error")
	assert.eq(err4:kind(), errors.INVALID, "empty password error kind")

	-- Empty salt should error
	local _, err5 = crypto.pbkdf2("password", "", 1000, 32)
	assert.not_nil(err5, "empty salt should error")
	assert.eq(err5:kind(), errors.INVALID, "empty salt error kind")

	-- Zero iterations should error
	local _, err6 = crypto.pbkdf2("password", "salt", 0, 32)
	assert.not_nil(err6, "zero iterations should error")
	assert.eq(err6:kind(), errors.INVALID, "zero iterations error kind")

	-- Zero key length should error
	local _, err7 = crypto.pbkdf2("password", "salt", 1000, 0)
	assert.not_nil(err7, "zero key length should error")
	assert.eq(err7:kind(), errors.INVALID, "zero key length error kind")

	-- constant_time_compare
	local eq = crypto.constant_time_compare("hello", "hello")
	assert.eq(eq, true, "equal strings compare equal")

	local neq = crypto.constant_time_compare("hello", "world")
	assert.eq(neq, false, "different strings compare unequal")

	local neq2 = crypto.constant_time_compare("hello", "hell")
	assert.eq(neq2, false, "different length strings compare unequal")

	return true
end

return { main = main }
