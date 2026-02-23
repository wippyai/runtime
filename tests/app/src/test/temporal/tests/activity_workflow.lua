-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")

local function main()
	local result, err = process.exec(
		"app.test.temporal.workflows:activity_workflow",
		"app.test.temporal:test_worker",
		{ name = "Temporal" }
	)

	assert.is_nil(err, "activity workflow exec should not error")
	assert.is_table(result, "result should be table")
	assert.is_table(result.activity_result, "activity result table")
	assert.eq(result.activity_result.message, "Hello from workflow", "activity message")
	assert.eq(result.activity_result.name, "Temporal", "activity name")
	assert.is_table(result.workflow_input, "workflow input table")

	return true
end

return { main = main }
