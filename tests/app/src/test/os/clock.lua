local assert = require("assert_primitives")

local function main()
-- os.clock() returns elapsed CPU time
	local c1 = os.clock()
	assert.is_number(c1, "os.clock() should return a number")
	assert.ok(c1 >= 0, "os.clock() should be non-negative")

	-- clock should increase over time
	local c2 = os.clock()
	assert.ok(c2 >= c1, "os.clock() should increase or stay same")

	return {success = true}
end

return {main = main}
