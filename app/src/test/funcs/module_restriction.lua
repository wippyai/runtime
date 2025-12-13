-- Test that verifies module restrictions work correctly.
-- Functions should only be able to require modules declared in their modules list.

local assert2 = require("assert2")
local funcs = require("funcs")

local function main(args)
    -- Call a function that tries to require an undeclared module
    -- This should fail because undeclared_module tries to require("json")
    -- but json is not in its modules list
    local result, err = funcs.call("app.test.funcs:undeclared_module", {})

    -- Should have an error
    assert2.not_nil(err, "calling function with undeclared module should return error")

    -- Error should mention the module restriction
    assert2.error_contains(err, "json", "error should mention the undeclared module name")

    return { success = true, error_received = tostring(err) }
end

return { main = main }
