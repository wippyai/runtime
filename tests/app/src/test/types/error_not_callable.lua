local assert = require("assert2")
local registry = require("registry")

local function test_call_number(): boolean
    local snap, _ = registry.snapshot()
    local changes = snap:changes()
    changes:create({
        id = "app.test.types:_err_call_num",
        kind = "function.lua",
        data = {
            source = [[
local function main(): boolean
    local x: number = 42
    local y = x()
    return true
end
return { main = main }]],
            method = "main"
        }
    })
    local ver, err = changes:apply()
    assert.is_nil(ver, "should reject calling number")
    assert.not_nil(err, "should return error")
    return true
end

local function test_call_string(): boolean
    local snap, _ = registry.snapshot()
    local changes = snap:changes()
    changes:create({
        id = "app.test.types:_err_call_str",
        kind = "function.lua",
        data = {
            source = [[
local function main(): boolean
    local x: string = "hello"
    local y = x()
    return true
end
return { main = main }]],
            method = "main"
        }
    })
    local ver, err = changes:apply()
    assert.is_nil(ver, "should reject calling string")
    assert.not_nil(err, "should return error")
    return true
end

local function test_call_boolean(): boolean
    local snap, _ = registry.snapshot()
    local changes = snap:changes()
    changes:create({
        id = "app.test.types:_err_call_bool",
        kind = "function.lua",
        data = {
            source = [[
local function main(): boolean
    local x: boolean = true
    local y = x()
    return true
end
return { main = main }]],
            method = "main"
        }
    })
    local ver, err = changes:apply()
    assert.is_nil(ver, "should reject calling boolean")
    assert.not_nil(err, "should return error")
    return true
end

local function main(): boolean
    test_call_number()
    test_call_string()
    test_call_boolean()
    return true
end

return { main = main }
