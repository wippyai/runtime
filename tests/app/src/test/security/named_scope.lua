-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert_primitives")
local security = require("security")

local function main()
-- Test named_scope API (registry lookup may fail if policies aren't loaded)
-- Also test scope operations that work without registry

-- Test new_scope basic functionality
	local scope = security.new_scope()
	assert.not_nil(scope, "new_scope should create scope")

	-- Test scope policies method
	local policies = scope:policies()
	assert.not_nil(policies, "policies should return table")
	assert.eq(type(policies), "table", "policies should be table type")
	assert.eq(#policies, 0, "new scope should have 0 policies")

	-- Test contains method with non-existent policy
	local contains = scope:contains("nonexistent:policy")
	assert.eq(contains, false, "scope should not contain nonexistent policy")

	-- Test named_scope - may return error if registry not available
	local named, err = security.named_scope("app.test.security:test_group")
	if named then
	-- If named_scope works, verify it returns a scope
		assert.is_nil(err, "no error when scope found")
		local named_policies = named:policies()
		assert.not_nil(named_policies, "named scope should have policies")
	else
	-- If named_scope fails, verify error is returned
		assert.not_nil(err, "should return error when scope not found")
	end

	return true
end

return { main = main }
