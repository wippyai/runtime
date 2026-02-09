local assert = require("assert2")
local funcs = require("funcs")

local function main()
	local point_in = {x = 7, y = 11}
	local point_out, point_err = funcs.call("app.test.wasm:encoder_component_echo_point", point_in)
	assert.is_nil(point_err, "echo-point should not error")
	assert.is_table(point_out, "echo-point returns table")
	assert.eq(point_out.x, 7, "echo-point x")
	assert.eq(point_out.y, 11, "echo-point y")

	local sum_out, sum_err = funcs.call("app.test.wasm:encoder_component_sum_list", {1, 2, 3, 4})
	assert.is_nil(sum_err, "sum-list should not error")
	assert.eq(sum_out, 10, "sum-list result")

	return true
end

return { main = main }
