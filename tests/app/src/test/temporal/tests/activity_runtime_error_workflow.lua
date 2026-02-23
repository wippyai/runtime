-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")
local errors = require("errors")

local function main()
	local result, err = process.exec(
		"app.test.temporal.workflows:activity_failure_modes_workflow",
		"app.test.temporal:test_worker",
		{ mode = "runtime" }
	)

	assert.is_nil(err, "runtime activity failure workflow should return result, not error")
	assert.is_table(result, "result should be table")
	assert.eq(result.mode, "runtime", "mode preserved")
	assert.eq(result.status, "error_captured", "status indicates captured error")
	assert.eq(result.error_kind, errors.INTERNAL, "runtime activity error mapped to INTERNAL")
	assert.contains(tostring(result.error_message), "runtime activity crash", "runtime activity message propagated")

	return true
end

return { main = main }
