local assert = require("assert2")

local function main()
	local result, err = process.exec(
		"app.test.temporal.workflows:concurrent_workflow",
		"app.test.temporal:test_worker",
		{ workers = 2, jobs = 4 }
	)

	assert.is_nil(err, "concurrent workflow exec should not error")
	assert.is_table(result, "result table")
	assert.eq(result.worker_count, 2, "worker count")
	assert.eq(result.job_count, 4, "job count")
	assert.eq(result.total, 20, "sum of jobs doubled")
	assert.eq(#result.processed, 4, "processed count")

	return true
end

return { main = main }
