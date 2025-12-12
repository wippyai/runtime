-- Test: Async call to process function
local assert = require("assert2")
local funcs = require("funcs")

local function main()
    -- Async call to process
    local future, err = funcs.async("app.test.processfunc:echo_process", "async input")
    assert.is_nil(err, "async no error")
    assert.not_nil(future, "future returned")

    -- Wait for result
    local result = future:await()
    assert.not_nil(result, "await returns result")
    assert.eq(result:get("ok"), true, "result ok")
    assert.eq(result:get("echo"), "async input", "async result matches input")

    return true
end

return { main = main }
