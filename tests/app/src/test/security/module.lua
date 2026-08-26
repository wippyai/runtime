-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert_primitives")
local security = require("security")

local function main()
-- Verify module loaded
	assert.neq(security, nil, "security module should load")

	-- Verify all expected functions exist
	assert.eq(type(security.actor), "function", "actor function should exist")
	assert.eq(type(security.scope), "function", "scope function should exist")
	assert.eq(type(security.can), "function", "can function should exist")
	assert.eq(type(security.policy), "function", "policy function should exist")
	assert.eq(type(security.named_scope), "function", "named_scope function should exist")
	assert.eq(type(security.new_scope), "function", "new_scope function should exist")
	assert.eq(type(security.new_actor), "function", "new_actor function should exist")
	assert.eq(type(security.token_store), "function", "token_store function should exist")

	-- The test command declares its execution identity and scope. Functions
	-- called by the runner inherit that frame unless they explicitly replace it.
	local actor = security.actor()
	assert.not_nil(actor, "test command actor should be inherited")
	assert.eq(actor:id(), "app:test_runner", "test command actor identity")

	local scope = security.scope()
	assert.not_nil(scope, "test command scope should be inherited")

	local allowed = security.can("read", "resource")
	assert.eq(type(allowed), "boolean", "can should return boolean")
	assert.eq(allowed, true, "test command policy should allow access")

	-- policy() returns policy when found, error when not
	local pol, err = security.policy("app.test.security:allow_all")
	if pol then
	-- Policy found
		assert.is_nil(err, "no error when policy found")
	else
	-- Policy not found is also valid in test context
		assert.not_nil(err, "should return error when policy not found")
	end

	-- named_scope() returns scope when found, error when not
	local scope2, err2 = security.named_scope("app.test.security:test_group")
	if scope2 then
	-- Scope found
		assert.is_nil(err2, "no error when scope found")
	else
	-- Scope not found is also valid in test context
		assert.not_nil(err2, "should return error when scope not found")
	end

	return true
end

return { main = main }
