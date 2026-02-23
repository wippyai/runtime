-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local registry = require("registry")

local function test_nil_to_nonnullable(): boolean
	local snap, _ = registry.snapshot()
	local changes = snap:changes()
	changes:create({
		id = "app.test.types:_err_nil_assign",
		kind = "function.lua",
		data = {
			source = [[
local function main(): boolean
    local x: number = nil
    return true
end
return { main = main }]],
			method = "main"
		}
	})
	local ver, err = changes:apply()
	assert.is_nil(ver, "should reject nil to number")
	assert.not_nil(err, "should return error")
	return true
end

local function test_optional_to_nonnullable(): boolean
	local snap, _ = registry.snapshot()
	local changes = snap:changes()
	changes:create({
		id = "app.test.types:_err_opt_assign",
		kind = "function.lua",
		data = {
			source = [[
local function get(): number?
    return nil
end
local function main(): boolean
    local x: number = get()
    return true
end
return { main = main }]],
			method = "main"
		}
	})
	local ver, err = changes:apply()
	assert.is_nil(ver, "should reject optional to non-nullable")
	assert.not_nil(err, "should return error")
	return true
end

local function main(): boolean
	test_nil_to_nonnullable()
	test_optional_to_nonnullable()
	return true
end

return { main = main }
