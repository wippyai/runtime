local assert = require("assert2")
local registry = require("registry")

local function test_unexpected_token(): boolean
	local snap, _ = registry.snapshot()
	local changes = snap:changes()
	changes:create({
		id = "app.test.types:_err_unexpected_token",
		kind = "function.lua",
		data = {
			source = [[local x = 1 @@ 2]],
			method = "main"
		}
	})
	local ver, err = changes:apply()
	assert.is_nil(ver, "should reject invalid token")
	assert.not_nil(err, "should return error")
	return true
end

local function test_unclosed_string(): boolean
	local snap, _ = registry.snapshot()
	local changes = snap:changes()
	changes:create({
		id = "app.test.types:_err_unclosed_string",
		kind = "function.lua",
		data = {
			source = [[local x = "unclosed
return x]],
			method = "main"
		}
	})
	local ver, err = changes:apply()
	assert.is_nil(ver, "should reject unclosed string")
	assert.not_nil(err, "should return error")
	return true
end

local function test_missing_end(): boolean
	local snap, _ = registry.snapshot()
	local changes = snap:changes()
	changes:create({
		id = "app.test.types:_err_missing_end",
		kind = "function.lua",
		data = {
			source = [[
local function main()
    if true then
        return 1
return { main = main }]],
			method = "main"
		}
	})
	local ver, err = changes:apply()
	assert.is_nil(ver, "should reject missing end")
	assert.not_nil(err, "should return error")
	return true
end

local function test_invalid_type_syntax(): boolean
	local snap, _ = registry.snapshot()
	local changes = snap:changes()
	changes:create({
		id = "app.test.types:_err_invalid_type",
		kind = "function.lua",
		data = {
			source = [[local x: number< = 42]],
			method = "main"
		}
	})
	local ver, err = changes:apply()
	assert.is_nil(ver, "should reject invalid type syntax")
	assert.not_nil(err, "should return error")
	return true
end

local function main(): boolean
	test_unexpected_token()
	test_unclosed_string()
	test_missing_end()
	test_invalid_type_syntax()
	return true
end

return { main = main }
