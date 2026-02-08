local assert = require("assert2")

local function main()
	local result, err = process.exec(
		"app.test.temporal.workflows:uuid_workflow",
		"app.test.temporal:test_worker",
		{ count = 3 }
	)

	assert.is_nil(err, "uuid workflow exec should not error")
	assert.is_table(result, "result table")
	assert.eq(result.count, 3, "count")
	assert.eq(#result.uuids, 3, "uuid count")
	assert.is_string(result.uuids[1], "uuid string")

	return true
end

return { main = main }
