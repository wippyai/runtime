-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")

local function main()
	local result, err = process.exec(
		"app.test.temporal.workflows:workflow_module_test",
		"app.test.temporal:test_worker"
	)

	assert.is_nil(err, "workflow module test exec should not error")
	assert.is_table(result, "result table")
	assert.is_table(result.info, "workflow info present")
	assert.ok(result.info.has_workflow_id, "workflow id present")
	assert.ok(result.info.has_run_id, "run id present")
	assert.ok(result.history_length > 0, "history length > 0")
	assert.ok(result.history_size >= 0, "history size >= 0")
	assert.ok(result.version_consistent, "version consistent")
	assert.is_table(result.exec_result, "exec child result")
	assert.ok(result.history_grew, "history grew")

	return true
end

return { main = main }
