-- Test: hash.fnv32/fnv64 functions
local assert = require("assert2")
local hash = require("hash")

local function main()
-- FNV32 tests
	local result = hash.fnv32("hello")
	assert.eq(type(result), "number", "fnv32 returns number")

	local h1 = hash.fnv32("test")
	local h2 = hash.fnv32("test")
	assert.eq(h1, h2, "fnv32 deterministic")

	h1 = hash.fnv32("hello")
	h2 = hash.fnv32("world")
	assert.neq(h1, h2, "fnv32 different for different inputs")

	-- FNV64 tests
	result = hash.fnv64("hello")
	assert.eq(type(result), "number", "fnv64 returns number")

	h1 = hash.fnv64("test")
	h2 = hash.fnv64("test")
	assert.eq(h1, h2, "fnv64 deterministic")

	h1 = hash.fnv64("hello")
	h2 = hash.fnv64("world")
	assert.neq(h1, h2, "fnv64 different for different inputs")

	return true
end

return { main = main }
