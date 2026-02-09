local assert = require("assert2")
local funcs = require("funcs")

local function main()
	local add_out, add_err = funcs.call("app.test.wasm:js_add_component", 5, 7)
	assert.is_nil(add_err, "js_add_component should not error")
	assert.eq(add_out, 12, "js add result")

	local mul_out, mul_err = funcs.call("app.test.wasm:js_multiply_component", 6, 7)
	assert.is_nil(mul_err, "js_multiply_component should not error")
	assert.eq(mul_out, 42, "js multiply result")

	local greet_out, greet_err = funcs.call("app.test.wasm:js_greet_component", "Wippy")
	assert.is_nil(greet_err, "js_greet_component should not error")
	assert.eq(greet_out, "Hello, Wippy!", "js greet result")

	return true
end

return { main = main }
