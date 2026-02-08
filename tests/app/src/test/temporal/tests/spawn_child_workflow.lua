local assert = require("assert2")

local function main()
	local result, err = process.exec(
		"app.test.temporal.workflows:spawn_child_workflow",
		"app.test.temporal:test_worker"
	)

	assert.is_nil(err, "spawn child workflow exec should not error")
	assert.is_table(result, "result should be table")
	assert.eq(result.status, "completed", "status completed")
	assert.eq(result.event_kind, process.event.EXIT, "exit event kind")
	assert.eq(result.event_from, result.child_pid, "event from child pid")
	assert.is_table(result.child_value, "child value table")
	assert.contains(result.child_value.received, "hello", "child received message")

	return true
end

return { main = main }
