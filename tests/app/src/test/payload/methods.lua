-- Test: payload methods
local assert = require("assert2")

local function main()
-- Test data method with LUA format
	local p = payload.new({key = "value", num = 42})
	local data = p:data()
	assert.not_nil(data, "data returns value")
	assert.eq(type(data), "table", "data is table")
	assert.eq(data.key, "value", "data.key matches")
	assert.eq(data.num, 42, "data.num matches")

	-- Test data with string
	local p_str = payload.new("hello world")
	local str_data = p_str:data()
	assert.eq(str_data, "hello world", "string data matches")

	-- Test data with number
	local p_num = payload.new(123.456)
	local num_data = p_num:data()
	assert.eq(num_data, 123.456, "number data matches")

	-- Test data with boolean
	local p_bool = payload.new(true)
	local bool_data = p_bool:data()
	assert.eq(bool_data, true, "boolean data matches")

	-- Test data with array
	local p_arr = payload.new({1, 2, 3})
	local arr_data = p_arr:data()
	assert.eq(#arr_data, 3, "array length matches")
	assert.eq(arr_data[1], 1, "array[1] matches")
	assert.eq(arr_data[2], 2, "array[2] matches")
	assert.eq(arr_data[3], 3, "array[3] matches")

	-- Test unmarshal method (same as data for LUA format)
	local p2 = payload.new({a = 1, b = 2})
	local unmarshaled = p2:unmarshal()
	assert.not_nil(unmarshaled, "unmarshal returns value")
	assert.eq(unmarshaled.a, 1, "unmarshaled.a matches")
	assert.eq(unmarshaled.b, 2, "unmarshaled.b matches")

	return true
end

return { main = main }
