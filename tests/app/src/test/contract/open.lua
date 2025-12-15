local assert = require("assert2")
local contract = require("contract")

local function main()
    -- Test contract module is loaded
    assert.not_nil(contract, "contract module loaded")
    assert.not_nil(contract.open, "contract.open exists")
    assert.eq(type(contract.open), "function", "contract.open is function")

    -- Test opening a contract by binding ID
    local instance, err = contract.open("app.test.contract:greeter_impl")
    assert.is_nil(err, "open greeter_impl no error")
    assert.not_nil(instance, "got instance")

    -- Verify instance implements the contract
    local is_greeter = contract.is(instance, "app.test.contract:greeter")
    assert.eq(is_greeter, true, "instance implements greeter contract")

    -- Test opening calculator binding
    local calc, err2 = contract.open("app.test.contract:calculator_impl")
    assert.is_nil(err2, "open calculator_impl no error")
    assert.not_nil(calc, "got calculator instance")

    return true
end

return { main = main }
