local assert = require("assert2")
local registry = require("registry")

local function test_string_plus_string(): boolean
	local snap, _ = registry.snapshot()
	local changes = snap:changes()
	changes:create({
		id = "app.test.types:_err_str_add",
		kind = "function.lua",
		data = {
			source = [[
local function main(): boolean
    local x: string = "a" + "b"
    return true
end
return { main = main }]],
			method = "main"
		}
	})
	local ver, err = changes:apply()
	assert.is_nil(ver, "should reject string + string")
	assert.not_nil(err, "should return error")
	return true
end

local function test_string_plus_number(): boolean
	local snap, _ = registry.snapshot()
	local changes = snap:changes()
	changes:create({
		id = "app.test.types:_err_str_num_add",
		kind = "function.lua",
		data = {
			source = [[
local function main(): boolean
    local x = "hello" + 42
    return true
end
return { main = main }]],
			method = "main"
		}
	})
	local ver, err = changes:apply()
	assert.is_nil(ver, "should reject string + number")
	assert.not_nil(err, "should return error")
	return true
end

local function test_boolean_arithmetic(): boolean
	local snap, _ = registry.snapshot()
	local changes = snap:changes()
	changes:create({
		id = "app.test.types:_err_bool_arith",
		kind = "function.lua",
		data = {
			source = [[
local function main(): boolean
    local x = true + false
    return true
end
return { main = main }]],
			method = "main"
		}
	})
	local ver, err = changes:apply()
	assert.is_nil(ver, "should reject boolean arithmetic")
	assert.not_nil(err, "should return error")
	return true
end

local function main(): boolean
	test_string_plus_string()
	test_string_plus_number()
	test_boolean_arithmetic()
	return true
end

return { main = main }
