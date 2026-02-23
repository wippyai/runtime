-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local contract = require("contract")

local function main()
-- Test opening contract with scope/context
	local scope = {
		user_id = "test-user-123",
		tenant = "acme"
	}

	local instance, err = contract.open("app.test.contract:greeter_impl", scope)
	assert.is_nil(err, "open with scope no error")
	assert.not_nil(instance, "got instance with scope")

	-- Instance should still work
	local result = instance:greet()
	assert.eq(result, "Hello, World!", "greet works with scope")

	return true
end

return { main = main }
