local assert = require("assert2")
local registry = require("registry")

local function test_missing_return_valid(): boolean
	local snap, _ = registry.snapshot()
	local changes = snap:changes()
	changes:create({
		id = "app.test.types:_valid_return",
		kind = "function.lua",
		data = {
			source = [[
local function main(): boolean
    return true
end
return { main = main }]],
			method = "main"
		}
	})
	local ver, err = changes:apply()
	assert.not_nil(ver, "should accept function with return: " .. tostring(err))
	return true
end

local function test_optional_return_nil(): boolean
	local snap, _ = registry.snapshot()
	local changes = snap:changes()
	changes:create({
		id = "app.test.types:_optional_nil",
		kind = "function.lua",
		data = {
			source = [[
local function get(): number?
    -- can implicitly return nil
end
local function main(): boolean
    return get() == nil
end
return { main = main }]],
			method = "main"
		}
	})
	local ver, err = changes:apply()
	assert.not_nil(ver, "should accept optional return: " .. tostring(err))
	return true
end

local function main(): boolean
	test_missing_return_valid()
	test_optional_return_nil()
	return true
end

return { main = main }
