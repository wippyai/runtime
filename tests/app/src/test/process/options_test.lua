-- Test: process.get_options and process.set_options
local assert = require("assert2")

local function main()
    -- Test get_options exists
    assert.not_nil(process.get_options, "process.get_options exists")
    assert.is_function(process.get_options, "process.get_options is function")

    -- Test set_options exists
    assert.not_nil(process.set_options, "process.set_options exists")
    assert.is_function(process.set_options, "process.set_options is function")

    -- Get current options (should return empty table)
    local opts = process.get_options()
    assert.not_nil(opts, "get_options returns value")
    assert.is_table(opts, "get_options returns table")

    -- Set empty options should succeed
    local ok, err = process.set_options({})
    assert.ok(ok, "set_options empty succeeds")
    assert.is_nil(err, "set_options empty no error")

    -- Set unsupported option should fail
    local ok2, err2 = process.set_options({ unsupported_option = true })
    assert.ok(not ok2, "set_options unsupported fails")
    assert.not_nil(err2, "set_options unsupported returns error")
    assert.contains(tostring(err2), "not supported", "error mentions not supported")

    return true
end

return { main = main }
