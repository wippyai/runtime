-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local errors = require("errors")

local function main()
	local result, err = process.exec(
		"app.test.temporal.workflows:spawn_error_child_workflow",
		"app.test.temporal:test_worker"
	)

	assert.is_nil(err, "spawn error child workflow should return result")
	assert.is_table(result, "result should be table")
	assert.eq(result.status, "completed", "status completed")
	assert.eq(result.event_kind, process.event.EXIT, "exit event kind")
	assert.eq(result.event_from, result.child_pid, "event from child pid")
	assert.eq(result.error_kind, errors.NOT_FOUND, "error kind propagated")
	assert.eq(result.error_retryable, false, "error retryable false")
	assert.contains(result.error_message, "intentional error", "error message")

	return true
end

return { main = main }
