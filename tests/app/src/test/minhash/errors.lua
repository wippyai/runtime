-- SPDX-License-Identifier: MPL-2.0

-- Test: minhash error handling
local assert = require("assert2")
local minhash = require("minhash")

local function invalid(msg, result, err)
	assert.is_nil(result, msg .. ": nil result")
	assert.not_nil(err, msg .. ": error returned")
	assert.eq(err:kind(), errors.INVALID, msg .. ": INVALID kind")
	assert.eq(err:retryable(), false, msg .. ": not retryable")
end

local function main()
	invalid("text not a string", minhash.sketch(1))
	invalid("unknown option", minhash.sketch("a", { width = 5 }))
	invalid("fractional option", minhash.sketch("a", { k = 1.5 }))
	invalid("stride not coprime with k", minhash.sketch("a", { stride = 64 }))
	invalid("bands * rows not k", minhash.signature("a", { k = 64 }))
	invalid("sketch not whole words", minhash.bands("abc"))
	invalid("sketch sizes differ", minhash.estimate(minhash.sketch("a b"), minhash.sketch("a b", { k = 64 })))
	return true
end

return { main = main }
