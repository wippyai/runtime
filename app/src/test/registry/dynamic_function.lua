local assert = require("assert2")
local registry = require("registry")
local funcs = require("funcs")

local function main()
    -- find function entries from registry
    local entries, err = registry.find({kind = "function.lua", type = "test"})
    assert.is_nil(err, "find functions no error")
    assert.not_nil(entries, "find returns entries")
    assert.ok(#entries > 0, "found test functions")

    -- find the echo function
    local echo_entry = nil
    for _, entry in ipairs(entries) do
        local id = registry.parse_id(entry.id)
        if id.name == "echo" then
            echo_entry = entry
            break
        end
    end
    assert.not_nil(echo_entry, "found echo function entry")

    -- entry.id is already the full function id string
    local func_id = echo_entry.id
    assert.eq(func_id, "app.test.funcs:echo", "function id correct")

    -- call the function dynamically
    local result, err = funcs.call(func_id, "dynamic test")
    assert.is_nil(err, "call dynamic function no error")
    assert.not_nil(result, "call returns result")
    assert.eq(result.ok, true, "result ok")
    assert.eq(result.echo, "dynamic test", "echo received input")

    -- get function entry directly
    local entry2, err2 = registry.get("app.test.funcs:echo")
    assert.is_nil(err2, "get function entry no error")
    assert.not_nil(entry2, "got function entry")
    assert.eq(entry2.kind, "function.lua", "entry is function.lua")

    -- call using entry id directly
    local result2, err2 = funcs.call(entry2.id, "from entry")
    assert.is_nil(err2, "call from entry no error")
    assert.eq(result2.echo, "from entry", "echo received from entry")

    return true
end

return { main = main }
