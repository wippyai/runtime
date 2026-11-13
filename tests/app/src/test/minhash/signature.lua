-- SPDX-License-Identifier: MPL-2.0

-- Test: minhash.signature/sketch/bands/estimate functions
local assert = require("assert2")
local minhash = require("minhash")

local function main()
	-- Golden vector: one token fills every bin with the same value.
	local sig, err = minhash.signature("Hello", { domain = "curriculum.lsh.v1" })
	assert.is_nil(err, "signature succeeds")
	assert.eq(sig.words, 1, "one word")
	assert.eq(sig.normalized_sha256, "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824", "normalized digest")
	local sketch = sig.sketch
	if not sketch then error("sketch expected") end
	assert.eq(#sketch, 512, "128 bins of 4 bytes")
	assert.eq(string.sub(sketch, 1, 4), "\000\109\244\226", "bin value 7206114 big-endian")
	assert.eq(sig.bands[1], "d62554cb38bcce38", "first band key")
	assert.eq(sig.bands[16], "7a46a8962ebcf026", "last band key")

	local keys = minhash.bands(sketch, { domain = "curriculum.lsh.v1" })
	assert.eq(keys[1], sig.bands[1], "bands agree with signature")

	assert.eq(minhash.sketch("HELLO"), sketch, "ASCII case is lowered")

	local empty = minhash.signature(" ,;")
	assert.eq(empty.words, 0, "no words")
	assert.is_nil(empty.sketch, "no sketch without tokens")
	assert.is_nil(empty.bands, "no bands without tokens")

	local a = minhash.sketch("local function fold(state, event) return apply(state, event) end")
	local b = minhash.sketch("local function fold(state, event) return apply(state, other) end")
	local e = minhash.estimate(a, b)
	assert.ok(e > 0 and e < 1, "near texts estimate between 0 and 1")
	assert.eq(minhash.estimate(a, a), 1, "identical sketches estimate 1")

	return true
end

return { main = main }
