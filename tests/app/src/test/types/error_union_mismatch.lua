local assert = require("assert2")
local registry = require("registry")

local function test_value_not_in_union(): boolean
    local snap, _ = registry.snapshot()
    local changes = snap:changes()
    changes:create({
        id = "app.test.types:_err_not_in_union",
        kind = "function.lua",
        data = {
            source = [[
local function main(): boolean
    local x: number | string = true
    return true
end
return { main = main }]],
            method = "main"
        }
    })
    local ver, err = changes:apply()
    assert.is_nil(ver, "should reject boolean not in union")
    assert.not_nil(err, "should return error")
    return true
end

local function test_table_not_in_union(): boolean
    local snap, _ = registry.snapshot()
    local changes = snap:changes()
    changes:create({
        id = "app.test.types:_err_tbl_union",
        kind = "function.lua",
        data = {
            source = [[
local function main(): boolean
    local x: number | string | boolean = {}
    return true
end
return { main = main }]],
            method = "main"
        }
    })
    local ver, err = changes:apply()
    assert.is_nil(ver, "should reject table not in union")
    assert.not_nil(err, "should return error")
    return true
end

local function main(): boolean
    test_value_not_in_union()
    test_table_not_in_union()
    return true
end

return { main = main }
