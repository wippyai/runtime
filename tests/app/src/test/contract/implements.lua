local assert = require("assert2")
local contract = require("contract")

local function main()
    -- Open greeter binding
    local instance, err = contract.open("app.test.contract:greeter_impl")
    assert.is_nil(err, "open greeter no error")
    assert.not_nil(instance, "got instance")

    -- Test contract.is() function
    local implements_greeter = contract.is(instance, "app.test.contract:greeter")
    assert.eq(implements_greeter, true, "implements greeter contract")

    local implements_calculator = contract.is(instance, "app.test.contract:calculator")
    assert.eq(implements_calculator, false, "does not implement calculator")

    return true
end

return { main = main }
