-- SPDX-License-Identifier: MPL-2.0

-- Test: hash.minhash signature/sketch/bands/similarity and streaming
local assert = require("assert2")
local hash = require("hash")

local function main()
	-- Golden vector: one token fills every bin with the same value.
	local sig, err = hash.minhash.signature("Hello", { domain = "curriculum.lsh.v1" })
	assert.is_nil(err, "signature succeeds")
	assert.eq(sig.tokens, 1, "one token")
	assert.eq(sig.normalized, "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824", "normalized digest")
	local sketch = sig.sketch
	if not sketch then error("sketch expected") end
	assert.eq(#sketch, 512, "128 bins of 4 bytes")
	assert.eq(string.sub(sketch, 1, 4), "\054\250\113\104", "full shingle hash 922382696 big-endian")
	assert.eq(sig.bands[1], "1323ba36e6c3cf7c", "first band key")
	assert.eq(sig.bands[16], "9db1073e93b38b13", "last band key")

	local keys = hash.minhash.bands(sketch, { domain = "curriculum.lsh.v1" })
	assert.eq(keys[1], sig.bands[1], "bands agree with signature")
	assert.eq(hash.minhash.sketch("HELLO"), sketch, "ASCII case is lowered")
	assert.eq(hash.minhash.sketch({ "hello" }), sketch, "token array equals text")

	-- Golden vector: a token longer than 64 KiB stays one token.
	local long = hash.minhash.signature(string.rep("X", 70000) .. " end", { domain = "curriculum.lsh.v1" })
	assert.eq(long.tokens, 2, "long token is whole")
	assert.eq(long.normalized, "6f90f9395933e4d31878cccd1b475626d71c256b085b2b094dc1d7048b934d63", "long normalized digest")
	assert.eq(long.bands[1], "080c0bf773a0e945", "long first band key")
	assert.eq(long.bands[16], "2f42637946149fdf", "long last band key")

	local empty = hash.minhash.signature(" ,;")
	assert.eq(empty.tokens, 0, "no tokens")
	assert.is_nil(empty.sketch, "no sketch without tokens")
	assert.is_nil(empty.bands, "no bands without tokens")
	assert.eq(empty.normalized, hash.sha256(""), "normalized of no tokens")

	local a = hash.minhash.sketch("local function fold(state, event) return apply(state, event) end")
	local b = hash.minhash.sketch("local function fold(state, event) return apply(state, other) end")
	local j = hash.minhash.similarity(a, b)
	assert.ok(j > 0 and j < 1, "near texts estimate between 0 and 1")
	assert.eq(hash.minhash.similarity(a, a), 1, "identical sketches estimate 1")
	assert.eq(hash.minhash.similarity(hash.minhash.sketch("a"), hash.minhash.sketch("b")), 0, "different singleton tokens remain distinct")

	-- Streaming equals one-shot when chunks split tokens.
	local text = string.rep("local function fold(state, event) return apply(state, event) end\n", 200)
	local whole = hash.minhash.signature(text)
	local m = hash.minhash.new()
	local pos = 1
	while pos <= #text do
		local n = (pos % 97) + 1
		assert.ok(m:update(string.sub(text, pos, pos + n - 1)), "update succeeds")
		pos = pos + n
	end
	local streamed = m:sum()
	assert.eq(streamed.sketch, whole.sketch, "streamed sketch")
	assert.eq(streamed.normalized, whole.normalized, "streamed normalized digest")
	assert.eq(streamed.tokens, whole.tokens, "streamed token count")
	m:reset()
	assert.eq(m:sum().tokens, 0, "reset clears the state")

	return true
end

return { main = main }
