-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert_primitives")
local security = require("security")
local funcs = require("funcs")

local function main()
-- Test that policies from _index.yaml are loaded into registry

-- Get allow_all policy - should be registered
	local allow_policy, err = security.policy("app.test.security:allow_all")
	assert.is_nil(err, "should get allow_all policy from registry: " .. tostring(err))
	assert.not_nil(allow_policy, "allow_all policy should exist in registry")

	-- Get deny_all policy
	local deny_policy, err2 = security.policy("app.test.security:deny_all")
	assert.is_nil(err2, "should get deny_all policy from registry: " .. tostring(err2))
	assert.not_nil(deny_policy, "deny_all policy should exist in registry")

	-- Get read_only policy
	local read_policy, err3 = security.policy("app.test.security:read_only")
	assert.is_nil(err3, "should get read_only policy from registry: " .. tostring(err3))
	assert.not_nil(read_policy, "read_only policy should exist in registry")

	-- Get actor_match policy
	local actor_policy, err4 = security.policy("app.test.security:actor_match")
	assert.is_nil(err4, "should get actor_match policy from registry: " .. tostring(err4))
	assert.not_nil(actor_policy, "actor_match policy should exist in registry")

	-- Get named scope (policy group) - test_group should contain allow_all
	local test_scope, scope_err = security.named_scope("app.test.security:test_group")
	assert.is_nil(scope_err, "should get test_group from registry: " .. tostring(scope_err))
	assert.not_nil(test_scope, "test_group scope should exist")

	local policies = test_scope:policies()
	assert.not_nil(policies, "test_group should have policies")
	assert.eq(#policies, 1, "test_group should have 1 policy")

	-- Verify the policy in the group
	assert.eq(test_scope:contains("app.test.security:allow_all"), true, "test_group should contain allow_all")

	-- Test scope with allow_all policy - should allow everything
	local actor = security.new_actor("registry_test_user")
	local allow_scope = security.new_scope():with(allow_policy)
	local result = allow_scope:evaluate(actor, "anything", "any_resource")
	assert.eq(result, "allow", "allow_all policy should allow")

	-- Test scope with deny_all policy - should deny everything
	local deny_scope = security.new_scope():with(deny_policy)
	local deny_result = deny_scope:evaluate(actor, "anything", "any_resource")
	assert.eq(deny_result, "deny", "deny_all policy should deny")

	-- Test scope with read_only policy
	local read_scope = security.new_scope():with(read_policy)
	local read_result = read_scope:evaluate(actor, "read", "resource")
	assert.eq(read_result, "allow", "read_only should allow read")

	local write_result = read_scope:evaluate(actor, "write", "resource")
	assert.eq(write_result, "undefined", "read_only returns undefined for non-matching action")

	-- Test actor conditional policy
	local admin_actor = security.new_actor("admin")
	local user_actor = security.new_actor("user")
	local actor_scope = security.new_scope():with(actor_policy)

	local admin_result = actor_scope:evaluate(admin_actor, "action", "resource")
	assert.eq(admin_result, "allow", "actor_match should allow admin")

	local user_result = actor_scope:evaluate(user_actor, "action", "resource")
	assert.eq(user_result, "undefined", "actor_match returns undefined for non-matching actor")

	-- Test funcs with injected security context using registry policy
	local call_result, call_err = funcs.new()
	:with_actor(actor)
	:with_scope(allow_scope)
	:call("app.test.security:verify_context")

	assert.is_nil(call_err, "call with registry policy should not error: " .. tostring(call_err))
	assert.not_nil(call_result, "call result should exist")
	assert.eq(call_result.has_scope, true, "called function should see scope")
	assert.eq(call_result.can_read, true, "allow_all: can_read should be true")
	assert.eq(call_result.can_write, true, "allow_all: can_write should be true")

	return true
end

return { main = main }
