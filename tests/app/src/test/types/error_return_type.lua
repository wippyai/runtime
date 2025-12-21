local assert = require("assert2")
local registry = require("registry")

local function test_wrong_return_type(): boolean
    local snap, _ = registry.snapshot()
    local changes = snap:changes()
    changes:create({
        id = "app.test.types:_err_wrong_ret",
        kind = "function.lua",
        data = {
            source = [[
local function get_number(): number
    return "oops"
end
local function main(): boolean
    local x = get_number()
    return true
end
return { main = main }]],
            method = "main"
        }
    })
    local ver, err = changes:apply()
    assert.is_nil(ver, "should reject wrong return type")
    assert.not_nil(err, "should return error")
    return true
end

local function test_missing_return(): boolean
    local snap, _ = registry.snapshot()
    local changes = snap:changes()
    changes:create({
        id = "app.test.types:_err_miss_ret",
        kind = "function.lua",
        data = {
            source = [[
local function get_number(): number
    -- no return
end
local function main(): boolean
    return true
end
return { main = main }]],
            method = "main"
        }
    })
    local ver, err = changes:apply()
    assert.is_nil(ver, "should reject missing return")
    assert.not_nil(err, "should return error")
    return true
end

local function main(): boolean
    test_wrong_return_type()
    test_missing_return()
    return true
end

return { main = main }
