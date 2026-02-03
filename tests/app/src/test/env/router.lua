local assert = require("assert_primitives")
local env = require("env")

return function()
-- Test that router can read OS env vars (PATH should exist)
	local val, err = env.get("PATH")
	assert.is_nil(err, "router should read PATH from OS storage")
	assert.not_nil(val, "PATH should exist")

	-- Test that router can write to memory storage
	local ok, err = env.set("ROUTER_TEST_VAR", "router_value")
	assert.is_nil(err, "router should write to memory storage")
	assert.eq(ok, true, "env.set should return true")

	-- Test that we can read the value back
	local val, err = env.get("ROUTER_TEST_VAR")
	assert.is_nil(err, "router should read from memory storage")
	assert.eq(val, "router_value", "value should match what was set")

	-- Test get_all includes both OS and memory vars
	local all, err = env.get_all()
	assert.is_nil(err, "env.get_all should not return error")
	assert.not_nil(all.PATH, "get_all should include OS vars")
	assert.eq(all.ROUTER_TEST_VAR, "router_value", "get_all should include memory vars")
end
