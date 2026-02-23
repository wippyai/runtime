-- SPDX-License-Identifier: MPL-2.0

-- Test: errors.new creates structured errors
local assert = require("assert2")

local function main()
-- Simple string message
	local e1 = errors.new("simple error")
	assert.ok(e1, "errors.new returns error")
	assert.eq(e1:message(), "simple error", "message matches")
	assert.eq(e1:kind(), "", "default kind is empty")
	assert.is_nil(e1:retryable(), "default retryable is nil")
	assert.is_nil(e1:details(), "default details is nil")

	-- Full table constructor
	local e2 = errors.new({
		message = "not found",
		kind = errors.NOT_FOUND,
		retryable = false,
		details = {
			resource = "user",
			id = 123
		}
	})
	assert.eq(e2:message(), "not found", "message from table")
	assert.eq(e2:kind(), errors.NOT_FOUND, "kind from table")
	assert.eq(e2:retryable(), false, "retryable from table")

	local d = e2:details()
	assert.ok(d, "details exist")
	assert.eq(d.resource, "user", "details.resource")
	assert.eq(d.id, 123, "details.id")

	return true
end

return { main = main }
