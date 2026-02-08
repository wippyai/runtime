-- Tests the workflow module functions
local workflow = require("workflow")
local time = require("time")

local function main(input)
	local results = {}

	-- Test 1: workflow.info() should return workflow info
	local info, err = workflow.info()
	if info then
		results.info = {
			has_workflow_id = info.workflow_id ~= nil and info.workflow_id ~= "",
			has_run_id = info.run_id ~= nil and info.run_id ~= "",
			workflow_type = info.workflow_type,
			task_queue = info.task_queue,
			namespace = info.namespace,
			attempt = info.attempt
		}
	else
		results.info_error = tostring(err)
	end

	-- Test 2: workflow.history_length() should return > 0
	local length, _ = workflow.history_length()
	results.history_length = length

	-- Test 3: workflow.history_size() should return >= 0
	local size, _ = workflow.history_size()
	results.history_size = size

	-- Test 4: workflow.version() for code versioning
	local version, err = workflow.version("test_change_v1", 1, 3)
	if err then
		results.version_error = tostring(err)
	else
		results.version = version
	end

	-- Test 5: workflow.exec() to execute child workflow
	local child_result, err = workflow.exec("app.test.temporal.workflows:child_workflow", {
		message = "hello from parent"
	})
	if err then
		results.exec_error = tostring(err)
	else
		results.exec_result = child_result
	end

	-- Test 6: Multiple version calls with same change_id should return same version
	local version2, _ = workflow.version("test_change_v1", 1, 3)
	results.version_consistent = (version == version2)

	-- Test 7: Different change_id can have different version
	local version3, _ = workflow.version("test_change_v2", 0, 5)
	results.version_different_change = version3

	-- Small sleep to accumulate some history
	time.sleep(50 * time.MILLISECOND)

	-- Check history again after some operations
	local length_after, _ = workflow.history_length()
	results.history_length_after = length_after
	results.history_grew = length_after > length

	return results
end

return main
