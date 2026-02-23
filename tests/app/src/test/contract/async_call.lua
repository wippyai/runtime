-- SPDX-License-Identifier: MPL-2.0

-- Test: async contract method calls
local assert = require("assert2")
local contract = require("contract")

local function main()
-- Open calculator
	local calc, err = contract.open("app.test.contract:calculator_impl")
	assert.is_nil(err, "open calculator no error")
	assert.not_nil(calc, "got calculator instance")

	-- Test async add
	local future1, err2 = calc:add_async(10, 20)
	assert.is_nil(err2, "add_async no error")
	assert.not_nil(future1, "got future from add_async")

	-- Test async multiply
	local future2, err3 = calc:multiply_async(5, 6)
	assert.is_nil(err3, "multiply_async no error")
	assert.not_nil(future2, "got future from multiply_async")

	-- Receive first result
	local p1 = future1:response():receive()
	assert.eq(p1:data(), 30, "add_async 10+20=30")

	-- Receive second result
	local p2 = future2:response():receive()
	assert.eq(p2:data(), 30, "multiply_async 5*6=30")

	-- Test multiple async calls in parallel
	local greeter, err6 = contract.open("app.test.contract:greeter_impl")
	assert.is_nil(err6, "open greeter no error")

	local f1, _ = greeter:greet_async()
	local f2, _ = greeter:greet_with_name_async("Bob")
	local f3, _ = calc:add_async(100, 200)

	-- Receive all
	local r1 = f1:response():receive()
	local r2 = f2:response():receive()
	local r3 = f3:response():receive()

	assert.eq(r1:data(), "Hello, World!", "parallel greet")
	assert.eq(r2:data(), "Hello, Bob!", "parallel greet_with_name")
	assert.eq(r3:data(), 300, "parallel add")

	return true
end

return { main = main }
