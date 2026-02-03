local assert = require("assert2")
local registry = require("registry")

local function test_string_to_number(): boolean
	local snap, _ = registry.snapshot()
	local changes = snap:changes()
	changes:create({
		id = "app.test.types:_err_str_num",
		kind = "function.lua",
		data = {
			source = [[
local function main(): boolean
    local x: number = "hello"
    return true
end
return { main = main }]],
			method = "main"
		}
	})
	local ver, err = changes:apply()
	if ver ~= nil then
		print("DEBUG: code was accepted, ver=" .. tostring(ver))
	end
	if err ~= nil then
		print("DEBUG: got error=" .. tostring(err))
	end
	assert.is_nil(ver, "should reject string to number")
	assert.not_nil(err, "should return type error")
	return true
end

local function test_number_to_string(): boolean
	local snap, _ = registry.snapshot()
	local changes = snap:changes()
	changes:create({
		id = "app.test.types:_err_num_str",
		kind = "function.lua",
		data = {
			source = [[
local function main(): boolean
    local x: string = 42
    return true
end
return { main = main }]],
			method = "main"
		}
	})
	local ver, err = changes:apply()
	assert.is_nil(ver, "should reject number to string")
	assert.not_nil(err, "should return type error")
	return true
end

local function test_boolean_to_number(): boolean
	local snap, _ = registry.snapshot()
	local changes = snap:changes()
	changes:create({
		id = "app.test.types:_err_bool_num",
		kind = "function.lua",
		data = {
			source = [[
local function main(): boolean
    local x: number = true
    return true
end
return { main = main }]],
			method = "main"
		}
	})
	local ver, err = changes:apply()
	assert.is_nil(ver, "should reject boolean to number")
	assert.not_nil(err, "should return type error")
	return true
end

local function test_table_to_number(): boolean
	local snap, _ = registry.snapshot()
	local changes = snap:changes()
	changes:create({
		id = "app.test.types:_err_tbl_num",
		kind = "function.lua",
		data = {
			source = [[
local function main(): boolean
    local x: number = {}
    return true
end
return { main = main }]],
			method = "main"
		}
	})
	local ver, err = changes:apply()
	assert.is_nil(ver, "should reject table to number")
	assert.not_nil(err, "should return type error")
	return true
end

local function main(): boolean
	test_string_to_number()
	test_number_to_string()
	test_boolean_to_number()
	test_table_to_number()
	return true
end

return { main = main }
