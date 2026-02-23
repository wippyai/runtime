-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")

local function main()
	local result, err = process.exec(
		"app.test.temporal.workflows:timed_workflow",
		"app.test.temporal:test_worker",
		{ steps = 2, delay_ms = 10 }
	)

	assert.is_nil(err, "timed workflow exec should not error")
	assert.is_table(result, "result table")
	assert.eq(result.steps, 2, "steps")
	assert.eq(result.delay_ms, 10, "delay")
	assert.eq(#result.timeline, 3, "timeline entries")
	assert.ok(result.total_elapsed_ms >= 20, "elapsed covers sleeps")

	return true
end

return { main = main }
