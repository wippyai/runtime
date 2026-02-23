-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local errors = require("errors")

local function main()
	local result, err = process.exec(
		"app.test.temporal.workflows:activity_failure_modes_workflow",
		"app.test.temporal:test_worker",
		{ mode = "missing" }
	)

	assert.is_nil(err, "missing activity workflow should return result, not error")
	assert.is_table(result, "result should be table")
	assert.eq(result.mode, "missing", "mode preserved")
	assert.eq(result.status, "error_captured", "status indicates captured error")
	assert.contains(tostring(result.activity), "missing_activity", "missing activity id preserved")
	assert.eq(result.error_kind, errors.NOT_FOUND, "missing activity maps to NOT_FOUND")
	assert.eq(result.error_retryable, true, "missing activity defaults to retryable")

	return true
end

return { main = main }
