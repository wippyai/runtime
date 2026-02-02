-- Test: contract error handling
local assert = require("assert2")
local contract = require("contract")

local function main()
-- Test contract.get with non-existent contract
	local bad_contract, err1 = contract.get("app.test.contract:nonexistent")
	assert.is_nil(bad_contract, "nonexistent contract returns nil")
	assert.not_nil(err1, "nonexistent contract returns error")

	-- Test contract.open with non-existent binding
	local bad_instance, err2 = contract.open("app.test.contract:nonexistent_binding")
	assert.is_nil(bad_instance, "nonexistent binding returns nil")
	assert.not_nil(err2, "nonexistent binding returns error")

	-- Test calling non-existent method on instance
	local greeter, err3 = contract.open("app.test.contract:greeter_impl")
	assert.is_nil(err3, "open greeter no error")
	assert.not_nil(greeter, "got greeter")

	-- Accessing non-existent method returns nil (not error)
	local bad_method = greeter.nonexistent_method
	assert.is_nil(bad_method, "nonexistent method is nil")

	-- contract.is requires userdata, passing table raises arg error (expected behavior)

	-- Test contract.get with empty string
	local empty_contract, err4 = contract.get("")
	assert.is_nil(empty_contract, "empty contract id returns nil")
	assert.not_nil(err4, "empty contract id returns error")

	-- Test contract.open with empty string
	local empty_instance, err5 = contract.open("")
	assert.is_nil(empty_instance, "empty binding id returns nil")
	assert.not_nil(err5, "empty binding id returns error")

	-- Test :method() on contract wrapper with empty name
	local calc, _ = contract.get("app.test.contract:calculator")
	if calc then
		local empty_method, err6 = calc:method("")
		assert.is_nil(empty_method, "empty method name returns nil")
		assert.not_nil(err6, "empty method name returns error")
	end

	return true
end

return { main = main }
