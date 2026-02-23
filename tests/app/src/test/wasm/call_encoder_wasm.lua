-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local funcs = require("funcs")

local function main()
	local rect_in = {
		["top-left"] = {x = 0, y = 0},
		["bottom-right"] = {x = 100, y = 50},
	}

	local rect_out, rect_err = funcs.call("app.test.wasm:encoder_component_echo_rectangle", rect_in)
	assert.is_nil(rect_err, "echo-rectangle should not error")
	assert.is_table(rect_out, "echo-rectangle returns table")
	assert.eq(rect_out["top-left"].x, 0, "top-left.x")
	assert.eq(rect_out["top-left"].y, 0, "top-left.y")
	assert.eq(rect_out["bottom-right"].x, 100, "bottom-right.x")
	assert.eq(rect_out["bottom-right"].y, 50, "bottom-right.y")

	local people_in = {
		{name = "Alice", age = 25},
		{name = "Bob", age = 15},
		{name = "Charlie", age = 30},
	}

	local adults_out, adults_err = funcs.call("app.test.wasm:encoder_component_filter_adults", people_in)
	assert.is_nil(adults_err, "filter-adults should not error")
	assert.is_table(adults_out, "filter-adults returns table")
	assert.eq(#adults_out, 2, "filter-adults output length")
	assert.eq(adults_out[1].name, "Alice", "first adult")
	assert.eq(adults_out[2].name, "Charlie", "second adult")

	local divide_ok, divide_ok_err = funcs.call("app.test.wasm:encoder_component_try_divide", 10, 2)
	assert.is_nil(divide_ok_err, "try-divide success should not error")
	assert.is_table(divide_ok, "try-divide success returns result table")
	assert.eq(divide_ok.ok, 5, "try-divide ok result")

	local divide_err, divide_err_call = funcs.call("app.test.wasm:encoder_component_try_divide", 10, 0)
	assert.is_nil(divide_err_call, "try-divide err variant should not return call error")
	assert.is_table(divide_err, "try-divide err returns result table")
	assert.is_table(divide_err.err, "try-divide err payload table")
	assert.eq(divide_err.err.code, 1, "try-divide err.code")
	assert.eq(divide_err.err.message, "division by zero", "try-divide err.message")

	return true
end

return { main = main }
