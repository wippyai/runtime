-- Test: contract.open with query parameters in binding ID
local assert = require("assert2")
local contract = require("contract")

local function main()
-- Test opening with query parameters (they become scope)
	local greeter, err = contract.open("app.test.contract:greeter_impl?user=alice&count=5")
	assert.is_nil(err, "open with query params no error")
	assert.not_nil(greeter, "got greeter instance")

	-- Instance should work normally
	local result = greeter:greet()
	assert.eq(result, "Hello, World!", "greet works with query params")

	-- Test with boolean query parameter
	local greeter2, err2 = contract.open("app.test.contract:greeter_impl?debug=true&verbose=false")
	assert.is_nil(err2, "open with bool params no error")
	assert.not_nil(greeter2, "got greeter2 instance")

	-- Test with numeric query parameter
	local calc, err3 = contract.open("app.test.contract:calculator_impl?precision=10&scale=2.5")
	assert.is_nil(err3, "open with numeric params no error")
	assert.not_nil(calc, "got calculator instance")

	-- Verify calculator still works
	local sum = calc:add(1, 2)
	assert.eq(sum, 3, "calc add works with params")

	-- Test combining query params with explicit scope table
	local greeter3, err4 = contract.open("app.test.contract:greeter_impl?base=query", {
		override = "table",
		extra = "value"
	})
	assert.is_nil(err4, "open with query + table no error")
	assert.not_nil(greeter3, "got greeter3 instance")

	return true
end

return { main = main }
