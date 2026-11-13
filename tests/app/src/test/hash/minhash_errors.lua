-- SPDX-License-Identifier: MPL-2.0

-- Test: hash.minhash error handling
local assert = require("assert2")
local hash = require("hash")

local function invalid(msg, result, err)
	assert.is_nil(result, msg .. ": nil result")
	assert.not_nil(err, msg .. ": error returned")
	assert.eq(err:kind(), errors.INVALID, msg .. ": INVALID kind")
	assert.eq(err:retryable(), false, msg .. ": not retryable")
end

local function main()
	invalid("input not a string", hash.minhash.sketch(1))
	invalid("token not a string", hash.minhash.sketch({ "a", 2 }))
	invalid("empty token", hash.minhash.sketch({ "" }))
	invalid("unknown option", hash.minhash.sketch("a", { width = 5 }))
	invalid("fixed constant as option", hash.minhash.sketch("a", { modulus = 7 }))
	invalid("fractional option", hash.minhash.sketch("a", { k = 1.5 }))
	invalid("k shares a factor with 55", hash.minhash.sketch("a", { k = 110 }))
	invalid("bands * rows not k", hash.minhash.signature("a", { k = 64 }))
	invalid("sketch not whole words", hash.minhash.bands("abc"))
	invalid("sketch sizes differ", hash.minhash.similarity(hash.minhash.sketch("a b"), hash.minhash.sketch("a b", { k = 64 })))
	invalid("new with invalid banding", hash.minhash.new({ k = 64 }))
	return true
end

return { main = main }
