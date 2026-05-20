-- SPDX-License-Identifier: MPL-2.0

-- Test: zstd dictionary training and dict-based encode/decode
local assert = require("assert2")

local function make_samples(prefix)
	local s = {}
	for i = 1, 80 do
		table.insert(s, string.format(
			'{"event":"%s","user_id":%d,"session":"sess-%04d","ts":17000000%02d}',
			prefix, i, i, i % 100))
	end
	return s
end

local function main()
	local compress = require("compress")

	-- 1. Train a dictionary from structurally-similar JSON-ish samples
	local samples = make_samples("click")
	local dict, terr = compress.zstd.train_dict(samples)
	assert.is_nil(terr, "train_dict should succeed")
	assert.not_nil(dict, "dict bytes returned")
	assert.ok(#dict > 64, "dict has reasonable size")

	-- 2. inspect_dict returns a populated table
	local info, ierr = compress.zstd.inspect_dict(dict)
	assert.is_nil(ierr, "inspect_dict should succeed")
	assert.not_nil(info, "info table returned")
	assert.ok(info.id > 0, "dict id is non-zero")
	assert.ok(info.content_size > 0, "dict content_size > 0")

	-- 3. Round-trip: encode with dict, decode with dict
	local payload = '{"event":"click","user_id":4242,"session":"sess-4242","ts":1700000099}'
	local enc, e1 = compress.zstd.encode(payload, { dict = dict, level = 5 })
	assert.is_nil(e1, "encode with dict")
	local dec, e2 = compress.zstd.decode(enc, { dict = dict })
	assert.is_nil(e2, "decode with dict")
	assert.eq(dec, payload, "dict round-trip preserves data")

	-- 4. Compression win: dict-encoded must be strictly smaller than dictionary-less
	local without, ew = compress.zstd.encode(payload)
	assert.is_nil(ew, "encode without dict")
	assert.ok(#enc < #without,
		string.format("dict should improve compression: %d >= %d", #enc, #without))

	-- 5. Decode without dict fails on dict-compressed frame
	local _, e3 = compress.zstd.decode(enc)
	assert.not_nil(e3, "decode without dict should fail")

	-- 6. Decode with the wrong dict fails
	local other_dict, e4 = compress.zstd.train_dict(make_samples("purchase"))
	assert.is_nil(e4, "train_dict for wrong-dict test")
	local _, e5 = compress.zstd.decode(enc, { dict = other_dict })
	assert.not_nil(e5, "decode with wrong dict should fail")

	-- 7. train_dict rejects bad input (type errors are caught by the linter)
	local _, e6 = compress.zstd.train_dict({})
	assert.not_nil(e6, "empty samples table is rejected")
	assert.eq(e6:kind(), errors.INVALID, "empty samples kind")

	local _, e8 = compress.zstd.train_dict({ "ab", "cd" })
	assert.not_nil(e8, "all-too-short samples rejected")
	assert.eq(e8:kind(), errors.INVALID, "too-short samples kind")

	-- 8. inspect_dict rejects bogus bytes
	local _, e9 = compress.zstd.inspect_dict("not a dictionary")
	assert.not_nil(e9, "bogus dict bytes rejected")
	assert.eq(e9:kind(), errors.INVALID, "bogus dict kind")

	-- 9. encode/decode reject invalid dict option
	local _, e10 = compress.zstd.encode(payload, { dict = "" })
	assert.not_nil(e10, "empty dict rejected on encode")
	assert.eq(e10:kind(), errors.INVALID, "empty dict kind")

	return true
end

return { main = main }
