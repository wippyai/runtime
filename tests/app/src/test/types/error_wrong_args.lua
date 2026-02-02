local assert = require("assert2")
local registry = require("registry")

local function test_too_few_args(): boolean
	local snap, _ = registry.snapshot()
	local changes = snap:changes()
	changes:create({
		id = "app.test.types:_err_few_args",
		kind = "function.lua",
		data = {
			source = [[
local function add(a: number, b: number): number
    return a + b
end
local function main(): boolean
    local x = add(1)
    return true
end
return { main = main }]],
			method = "main"
		}
	})
	local ver, err = changes:apply()
	assert.is_nil(ver, "should reject too few args")
	assert.not_nil(err, "should return error")
	return true
end

local function test_too_many_args(): boolean
	local snap, _ = registry.snapshot()
	local changes = snap:changes()
	changes:create({
		id = "app.test.types:_err_many_args",
		kind = "function.lua",
		data = {
			source = [[
local function add(a: number, b: number): number
    return a + b
end
local function main(): boolean
    local x = add(1, 2, 3)
    return true
end
return { main = main }]],
			method = "main"
		}
	})
	local ver, err = changes:apply()
	assert.is_nil(ver, "should reject too many args")
	assert.not_nil(err, "should return error")
	return true
end

local function test_wrong_arg_type(): boolean
	local snap, _ = registry.snapshot()
	local changes = snap:changes()
	changes:create({
		id = "app.test.types:_err_wrong_type",
		kind = "function.lua",
		data = {
			source = [[
local function greet(name: string): string
    return "Hello " .. name
end
local function main(): boolean
    local x = greet(42)
    return true
end
return { main = main }]],
			method = "main"
		}
	})
	local ver, err = changes:apply()
	assert.is_nil(ver, "should reject wrong arg type")
	assert.not_nil(err, "should return error")
	return true
end

local function main(): boolean
	test_too_few_args()
	test_too_many_args()
	test_wrong_arg_type()
	return true
end

return { main = main }
