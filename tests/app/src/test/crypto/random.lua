-- SPDX-License-Identifier: MPL-2.0

-- Test: crypto.random functions
local assert = require("assert2")

local function main()
	local crypto = require("crypto")

	-- random.bytes
	local bytes, err = crypto.random.bytes(16)
	assert.is_nil(err, "random.bytes should not error")
	assert.not_nil(bytes, "random.bytes returns data")
	assert.eq(#bytes, 16, "random.bytes returns correct length")

	-- Different calls produce different results
	local bytes2, _ = crypto.random.bytes(16)
	assert.neq(bytes, bytes2, "random.bytes produces different output")

	-- random.bytes invalid length
	local _, err2 = crypto.random.bytes(0)
	assert.not_nil(err2, "zero length should error")

	local _, err3 = crypto.random.bytes(-1)
	assert.not_nil(err3, "negative length should error")

	-- random.string default charset
	local str, err4 = crypto.random.string(32)
	assert.is_nil(err4, "random.string should not error")
	assert.eq(#str, 32, "random.string returns correct length")

	-- random.string custom charset
	local hex, err5 = crypto.random.string(16, "0123456789abcdef")
	assert.is_nil(err5, "random.string with charset should not error")
	assert.eq(#hex, 16, "random.string with charset returns correct length")

	-- random.string empty charset
	local _, err6 = crypto.random.string(10, "")
	assert.not_nil(err6, "empty charset should error")

	-- random.uuid
	local uuid, err7 = crypto.random.uuid()
	assert.is_nil(err7, "random.uuid should not error")
	assert.eq(#uuid, 36, "uuid is 36 chars")
	assert.contains(uuid, "-", "uuid contains dashes")

	return true
end

return { main = main }
