local assert = require("assert2")

local function main()
	local result, err = process.exec(
		"app.test.temporal.workflows:hello_workflow",
		"app.test.temporal:test_worker",
		{ name = "Temporal" }
	)

	assert.is_nil(err, "process.exec should not error")
	assert.is_table(result, "expected workflow result table")
	assert.eq(result.status, "completed", "workflow completed")
	assert.contains(result.message, "Temporal", "message contains name")

	return true
end

return { main = main }
