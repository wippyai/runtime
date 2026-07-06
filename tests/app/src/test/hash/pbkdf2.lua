-- SPDX-License-Identifier: MPL-2.0

-- Test: hash.pbkdf2 function
local assert = require("assert2")
local hash = require("hash")

local function main()
	local key, err = hash.pbkdf2("password", "salt", 1000, 32)
	assert.is_nil(err, "pbkdf2 should not error")
	assert.not_nil(key, "pbkdf2 returns key")
	assert.eq(#key, 32, "pbkdf2 returns correct length")

	local key2, err2 = hash.pbkdf2("password", "salt", 1000, 32)
	assert.is_nil(err2, "pbkdf2 repeat should not error")
	assert.eq(key, key2, "pbkdf2 is deterministic")

	local key3, err3 = hash.pbkdf2("other", "salt", 1000, 32)
	assert.is_nil(err3, "pbkdf2 different password should not error")
	assert.neq(key, key3, "different password produces different key")

	local key4, err4 = hash.pbkdf2("password", "salt", 1000, 64, "sha512")
	assert.is_nil(err4, "pbkdf2 sha512 should not error")
	assert.eq(#key4, 64, "pbkdf2 sha512 returns correct length")

	local _, err5 = hash.pbkdf2("password", "salt", 1000, 32, "md5")
	assert.not_nil(err5, "invalid pbkdf2 algo should error")
	assert.eq(err5:kind(), errors.INVALID, "invalid pbkdf2 algo error kind")

	return true
end

return { main = main }
