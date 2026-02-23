-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local errors = require("errors")

local function main()
	local result, err = process.exec(
		"app.test.temporal.workflows:error_child_workflow",
		"app.test.temporal:test_worker"
	)

	assert.is_nil(result, "result should be nil on error")
	assert.not_nil(err, "exec should return error")
	if type(err) == "userdata" or type(err) == "table" then
		assert.eq(err:kind(), errors.NOT_FOUND, "error kind")
		assert.eq(err:retryable(), false, "error retryable")
		assert.contains(err:message(), "intentional error", "error message")
	end

	return true
end

return { main = main }
