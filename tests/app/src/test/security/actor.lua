local assert = require("assert_primitives")
local security = require("security")
local funcs = require("funcs")

local function main()
-- Create an actor with ID and metadata
	local actor = security.new_actor("test_user", {
		role = "admin",
		department = "engineering"
	})
	assert.not_nil(actor, "actor should be created")
	assert.eq(actor:id(), "test_user", "actor id should match")

	local meta = actor:meta()
	assert.not_nil(meta, "actor meta should exist")
	assert.eq(meta.role, "admin", "actor role should match")
	assert.eq(meta.department, "engineering", "actor department should match")

	-- Call verify_context with the actor injected
	local result, err = funcs.new()
	:with_actor(actor)
	:call("app.test.security:verify_context")

	assert.is_nil(err, "call should not error")
	assert.not_nil(result, "result should exist")
	assert.eq(result.has_actor, true, "called function should see actor")
	assert.eq(result.actor_id, "test_user", "called function should see correct actor id")
	assert.not_nil(result.actor_meta, "called function should see actor meta")
	assert.eq(result.actor_meta.role, "admin", "called function should see actor role")

	return true
end

return { main = main }
