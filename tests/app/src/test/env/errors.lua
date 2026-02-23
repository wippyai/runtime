-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert_primitives")
local env = require("env")

return function()
-- Test getting non-existent variable returns error
	local val, err = env.get("NONEXISTENT_VAR_XYZ_123")
	assert.not_nil(err, "env.get with non-existent var should return error")
	assert.is_nil(val, "env.get should return nil for non-existent var")

	-- Test empty key returns error as second return value
	local val2, err2 = env.get("")
	assert.is_nil(val2, "env.get with empty key should return nil")
	assert.not_nil(err2, "env.get with empty key should return error")

	-- Test empty key for set returns error as second return value
	local ok, err3 = env.set("", "value")
	assert.is_nil(ok, "env.set with empty key should return nil")
	assert.not_nil(err3, "env.set with empty key should return error")
end
