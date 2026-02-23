-- SPDX-License-Identifier: MPL-2.0

-- Test: gzip encode/decode round-trip
local assert = require("assert2")

local function main()
	local compress = require("compress")

	-- Basic round-trip
	local original = "Hello, World! This is a test string for compression."
	local encoded, err = compress.gzip.encode(original)
	assert.is_nil(err, "encode should not error")
	assert.not_nil(encoded, "encoded data returned")
	assert.neq(encoded, original, "encoded differs from original")

	local decoded, err2 = compress.gzip.decode(encoded)
	assert.is_nil(err2, "decode should not error")
	assert.eq(decoded, original, "round-trip preserves data")

	-- Large data
	local large = string.rep("abcdefghij", 1000)
	local enc_large, err3 = compress.gzip.encode(large)
	assert.is_nil(err3, "encode large data")
	assert.ok(#enc_large < #large, "compression reduces size")

	local dec_large, err4 = compress.gzip.decode(enc_large)
	assert.is_nil(err4, "decode large data")
	assert.eq(dec_large, large, "large data round-trip")

	-- Binary data
	local binary = "\x00\x01\x02\xff\xfe\xfd"
	local enc_bin, err5 = compress.gzip.encode(binary)
	assert.is_nil(err5, "encode binary")
	local dec_bin, err6 = compress.gzip.decode(enc_bin)
	assert.is_nil(err6, "decode binary")
	assert.eq(dec_bin, binary, "binary round-trip")

	return true
end

return { main = main }
