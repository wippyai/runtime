-- Test: opening contract via wrapper (contract.get():open())
local assert = require("assert2")
local contract = require("contract")

local function main()
    -- Get contract wrapper
    local greeter_contract, err = contract.get("app.test.contract:greeter")
    assert.is_nil(err, "get greeter contract no error")
    assert.not_nil(greeter_contract, "got greeter contract")

    -- Open specific binding via wrapper
    local instance, err2 = greeter_contract:open("app.test.contract:greeter_impl")
    assert.is_nil(err2, "wrapper open no error")
    assert.not_nil(instance, "got instance via wrapper")

    -- Verify it works
    local result = instance:greet()
    assert.eq(result, "Hello, World!", "greet via wrapper-opened instance")

    -- Test opening with scope via wrapper
    local instance2, err3 = greeter_contract:open("app.test.contract:greeter_impl", {
        tenant = "test-tenant"
    })
    assert.is_nil(err3, "wrapper open with scope no error")
    assert.not_nil(instance2, "got instance2")

    local result2 = instance2:greet_with_name("Charlie")
    assert.eq(result2, "Hello, Charlie!", "greet_with_name via wrapper")

    -- Test contract.is on wrapper-opened instance
    local is_greeter = contract.is(instance, "app.test.contract:greeter")
    assert.eq(is_greeter, true, "wrapper instance implements greeter")

    local is_calc = contract.is(instance, "app.test.contract:calculator")
    assert.eq(is_calc, false, "wrapper instance does not implement calculator")

    -- Test calculator via wrapper
    local calc_contract, err4 = contract.get("app.test.contract:calculator")
    assert.is_nil(err4, "get calculator contract no error")

    local calc, err5 = calc_contract:open("app.test.contract:calculator_impl")
    assert.is_nil(err5, "open calculator via wrapper no error")

    local sum = calc:add(7, 8)
    assert.eq(sum, 15, "add via wrapper-opened calculator")

    return true
end

return { main = main }
