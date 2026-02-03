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

	-- Without security context, actor() returns nil
	local actor = security.actor()
	assert.eq(actor, nil, "actor should be nil without security context")

	-- Without security context, scope() returns nil
	local scope = security.scope()
	assert.eq(scope, nil, "scope should be nil without security context")

	-- Without security context, can() returns false
	local allowed = security.can("read", "resource")
	assert.eq(type(allowed), "boolean", "can should return boolean")
	assert.eq(allowed, false, "can should return false without security context")

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
