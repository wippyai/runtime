-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local errors = require("errors")

local function main()
	local result, err = process.exec(
		"app.test.temporal.workflows:activity_error_workflow",
		"app.test.temporal:test_worker"
	)

	assert.is_nil(err, "activity error workflow should return result, not error")
	assert.is_table(result, "result should be table")
	assert.eq(result.status, "error_captured", "status indicates captured error")
	assert.eq(result.error_kind, errors.NOT_FOUND, "error kind propagated")
	assert.eq(result.error_retryable, false, "retryable false")
	assert.contains(result.error_message, "resource not found", "error message propagated")

	return true
end

return { main = main }
