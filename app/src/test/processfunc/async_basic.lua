-- Test: Async call to process function
local assert = require("assert2")
local funcs = require("funcs")

local function main()
    -- Async call to process
    local future, err = funcs.async("app.test.processfunc:echo_process", "async input")
    assert.is_nil(err, "async no error")
    assert.not_nil(future, "future returned")

    -- Wait for result
    local payload, ok = future:response():receive()
    assert.eq(ok, true, "receive ok")
    assert.not_nil(payload, "receive returns payload")
    local result = payload:data()
    assert.eq(result.ok, true, "result ok")
    assert.eq(result.echo, "async input", "async result matches input")

    return true
end

return { main = main }
