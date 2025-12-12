-- Test: async contract method calls
local assert = require("assert2")
local contract = require("contract")
local time = require("time")

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

    -- Await first result
    local result1 = future1:await()
    assert.eq(result1:value(), 30, "add_async 10+20=30")

    -- Await second result
    local result2 = future2:await()
    assert.eq(result2:value(), 30, "multiply_async 5*6=30")

    -- Test multiple async calls in parallel
    local greeter, err6 = contract.open("app.test.contract:greeter_impl")
    assert.is_nil(err6, "open greeter no error")

    local f1, _ = greeter:greet_async()
    local f2, _ = greeter:greet_with_name_async("Bob")
    local f3, _ = calc:add_async(100, 200)

    -- Await all
    local r1 = f1:await()
    local r2 = f2:await()
    local r3 = f3:await()

    assert.eq(r1:value(), "Hello, World!", "parallel greet")
    assert.eq(r2:value(), "Hello, Bob!", "parallel greet_with_name")
    assert.eq(r3:value(), 300, "parallel add")

    return true
end

return { main = main }
