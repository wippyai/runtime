-- Test: Compression level options
local assert = require("assert2")

local function main()
	local compress = require("compress")
	local original = string.rep("Hello, World! ", 100)

	-- gzip levels (1-9)
	local enc_low, err1 = compress.gzip.encode(original, { level = 1 })
	assert.is_nil(err1, "gzip level 1")
	local enc_high, err2 = compress.gzip.encode(original, { level = 9 })
	assert.is_nil(err2, "gzip level 9")
	assert.ok(#enc_high <= #enc_low, "higher level = better compression")

	-- Verify both decode correctly
	local dec1, _ = compress.gzip.decode(enc_low)
	local dec2, _ = compress.gzip.decode(enc_high)
	assert.eq(dec1, original, "level 1 decodes")
	assert.eq(dec2, original, "level 9 decodes")

	-- deflate levels
	enc_low, err1 = compress.deflate.encode(original, { level = 1 })
	assert.is_nil(err1, "deflate level 1")
	enc_high, err2 = compress.deflate.encode(original, { level = 9 })
	assert.is_nil(err2, "deflate level 9")

	-- zlib levels
	enc_low, err1 = compress.zlib.encode(original, { level = 1 })
	assert.is_nil(err1, "zlib level 1")
	enc_high, err2 = compress.zlib.encode(original, { level = 9 })
	assert.is_nil(err2, "zlib level 9")

	-- brotli levels (0-11)
	enc_low, err1 = compress.brotli.encode(original, { level = 0 })
	assert.is_nil(err1, "brotli level 0")
	enc_high, err2 = compress.brotli.encode(original, { level = 11 })
	assert.is_nil(err2, "brotli level 11")

	-- zstd levels (1-22)
	enc_low, err1 = compress.zstd.encode(original, { level = 1 })
	assert.is_nil(err1, "zstd level 1")
	enc_high, err2 = compress.zstd.encode(original, { level = 22 })
	assert.is_nil(err2, "zstd level 22")

	-- Invalid levels
	local result, err = compress.gzip.encode(original, { level = 0 })
	assert.is_nil(result, "gzip level 0 invalid")
	assert.not_nil(err, "gzip level 0 returns error")

	result, err = compress.gzip.encode(original, { level = 10 })
	assert.is_nil(result, "gzip level 10 invalid")
	assert.not_nil(err, "gzip level 10 returns error")

	result, err = compress.brotli.encode(original, { level = 12 })
	assert.is_nil(result, "brotli level 12 invalid")
	assert.not_nil(err, "brotli level 12 returns error")

	result, err = compress.zstd.encode(original, { level = 23 })
	assert.is_nil(result, "zstd level 23 invalid")
	assert.not_nil(err, "zstd level 23 returns error")

	return true
end

return { main = main }
