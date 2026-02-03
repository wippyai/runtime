local assert = require("assert2")
local contract = require("contract")

local function main()
-- Open greeter contract
	local greeter, err = contract.open("app.test.contract:greeter_impl")
	assert.is_nil(err, "open greeter no error")
	assert.not_nil(greeter, "got greeter instance")

	-- Call greet method (no args)
	local result, err2 = greeter:greet()
	assert.is_nil(err2, "greet no error")
	assert.eq(result, "Hello, World!", "greet returns greeting")

	-- Call greet_with_name method
	local result2, err3 = greeter:greet_with_name("Alice")
	assert.is_nil(err3, "greet_with_name no error")
	assert.eq(result2, "Hello, Alice!", "greet_with_name returns personalized greeting")

	-- Test without name
	local result3, err4 = greeter:greet_with_name()
	assert.is_nil(err4, "greet_with_name no args no error")
	assert.eq(result3, "Hello, Anonymous!", "greet_with_name uses default")

	-- Open calculator and test
	local calc, err5 = contract.open("app.test.contract:calculator_impl")
	assert.is_nil(err5, "open calculator no error")

	-- Test add
	local sum, err6 = calc:add(3, 5)
	assert.is_nil(err6, "add no error")
	assert.eq(sum, 8, "add 3+5=8")

	-- Test multiply
	local product, err7 = calc:multiply(4, 7)
	assert.is_nil(err7, "multiply no error")
	assert.eq(product, 28, "multiply 4*7=28")

	return true
end

return { main = main }
