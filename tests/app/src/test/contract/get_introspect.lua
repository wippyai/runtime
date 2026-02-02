-- Test: contract.get() and introspection methods
local assert = require("assert2")
local contract = require("contract")

local function main()
-- Test contract.get()
	local greeter, err = contract.get("app.test.contract:greeter")
	assert.is_nil(err, "get greeter contract no error")
	assert.not_nil(greeter, "got greeter contract")

	-- Test :id()
	local id = greeter:id()
	assert.eq(id, "app.test.contract:greeter", "contract id matches")

	-- Test :methods()
	local methods = greeter:methods()
	assert.not_nil(methods, "methods returned")
	assert.eq(#methods, 2, "greeter has 2 methods")

	-- Verify method names
	local method_names = {}
	for _, m in ipairs(methods) do
		method_names[m.name] = true
		assert.not_nil(m.name, "method has name")
		assert.not_nil(m.description, "method has description")
	end
	assert.eq(method_names["greet"], true, "has greet method")
	assert.eq(method_names["greet_with_name"], true, "has greet_with_name method")

	-- Test :method() for specific method
	local greet_method, err2 = greeter:method("greet")
	assert.is_nil(err2, "get greet method no error")
	assert.not_nil(greet_method, "got greet method")
	assert.eq(greet_method.name, "greet", "method name is greet")
	assert.eq(greet_method.description, "Returns a greeting message", "method description")

	-- Test :method() for non-existent method
	local bad_method, err3 = greeter:method("nonexistent")
	assert.is_nil(bad_method, "nonexistent method returns nil")
	assert.not_nil(err3, "nonexistent method returns error")

	-- Test :implementations()
	local impls, err4 = greeter:implementations()
	assert.is_nil(err4, "implementations no error")
	assert.not_nil(impls, "got implementations")
	assert.eq(#impls >= 1, true, "has at least one implementation")

	-- Test calculator contract
	local calc, err5 = contract.get("app.test.contract:calculator")
	assert.is_nil(err5, "get calculator no error")

	local calc_methods = calc:methods()
	assert.eq(#calc_methods, 2, "calculator has 2 methods")

	return true
end

return { main = main }
