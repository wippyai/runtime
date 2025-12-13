-- Helper function that calls another function via funcs.new():call() WITHOUT with_context
-- This tests whether context is automatically inherited through nested funcs calls
local funcs = require("funcs")

local function main(keys)
    -- Call ctx_reader without explicit with_context()
    -- If context inheritance works, ctx_reader should still see the context values
    local result, err = funcs.new():call("app.test.ctx:ctx_reader", keys)
    if err then
        error("nested call failed: " .. tostring(err))
    end
    return result
end

return { main = main }
