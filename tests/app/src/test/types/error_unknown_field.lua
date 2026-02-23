-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local registry = require("registry")

local function test_unknown_record_field(): boolean
	local snap, _ = registry.snapshot()
	local changes = snap:changes()
	changes:create({
		id = "app.test.types:_err_unknown_field",
		kind = "function.lua",
		data = {
			source = [[
type Person = { name: string, age: number }
local function main(): boolean
    local p: Person = { name = "Alice", age = 30 }
    local x = p.unknown
    return true
end
return { main = main }]],
			method = "main"
		}
	})
	local ver, err = changes:apply()
	assert.is_nil(ver, "should reject unknown field")
	assert.not_nil(err, "should return error")
	return true
end

local function test_field_on_primitive(): boolean
	local snap, _ = registry.snapshot()
	local changes = snap:changes()
	changes:create({
		id = "app.test.types:_err_field_prim",
		kind = "function.lua",
		data = {
			source = [[
local function main(): boolean
    local x: number = 42
    local y = x.field
    return true
end
return { main = main }]],
			method = "main"
		}
	})
	local ver, err = changes:apply()
	assert.is_nil(ver, "should reject field on primitive")
	assert.not_nil(err, "should return error")
	return true
end

local function main(): boolean
	test_unknown_record_field()
	test_field_on_primitive()
	return true
end

return { main = main }
