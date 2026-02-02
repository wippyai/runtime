-- Test: contract.find_implementations()
local assert = require("assert2")
local contract = require("contract")

local function main()
-- Find implementations of greeter contract
	local greeter_impls, err = contract.find_implementations("app.test.contract:greeter")
	assert.is_nil(err, "find greeter implementations no error")
	assert.not_nil(greeter_impls, "got greeter implementations")
	assert.eq(type(greeter_impls), "table", "implementations is table")
	assert.eq(#greeter_impls >= 1, true, "has at least one greeter implementation")

	-- Verify greeter_impl is in the list
	local found_greeter = false
	for _, impl in ipairs(greeter_impls) do
		if impl == "app.test.contract:greeter_impl" then
			found_greeter = true
			break
		end
	end
	assert.eq(found_greeter, true, "greeter_impl in implementations list")

	-- Find implementations of calculator contract
	local calc_impls, err2 = contract.find_implementations("app.test.contract:calculator")
	assert.is_nil(err2, "find calculator implementations no error")
	assert.not_nil(calc_impls, "got calculator implementations")
	assert.eq(#calc_impls >= 1, true, "has at least one calculator implementation")

	-- Verify calculator_impl is in the list
	local found_calc = false
	for _, impl in ipairs(calc_impls) do
		if impl == "app.test.contract:calculator_impl" then
			found_calc = true
			break
		end
	end
	assert.eq(found_calc, true, "calculator_impl in implementations list")

	-- Test non-existent contract
	local bad_impls, err3 = contract.find_implementations("app.test.contract:nonexistent")
	-- Should return empty list or error depending on implementation
	if err3 == nil then
		assert.eq(type(bad_impls), "table", "nonexistent returns table")
		assert.eq(#bad_impls, 0, "nonexistent has no implementations")
	end

	return true
end

return { main = main }
