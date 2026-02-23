-- SPDX-License-Identifier: MPL-2.0

-- Test: HTML module error handling
local assert = require("assert_primitives")

local function main()
	local html = require("html")

	-- Test invalid regex pattern in matching
	local policy = html.sanitize.new_policy()
	policy:allow_elements("div")
	local builder = policy:allow_attrs("class")
	local result, err = builder:matching("[invalid")
	assert.is_nil(result, "invalid regex returns nil")
	assert.not_nil(err, "error returned for invalid regex")
	assert.eq(err:kind(), errors.INVALID, "error kind is INVALID")
	assert.eq(err:retryable(), false, "not retryable")

	-- Test valid regex works
	local policy2 = html.sanitize.new_policy()
	policy2:allow_elements("div")
	local builder2 = policy2:allow_attrs("class")
	local result2, err2 = builder2:matching("^[a-z]+$")
	assert.not_nil(result2, "valid regex returns builder")
	assert.is_nil(err2, "no error for valid regex")

	return true
end

return { main = main }
