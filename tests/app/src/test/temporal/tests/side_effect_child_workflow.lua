-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")

local function main()
	local result, err = process.exec(
		"app.test.temporal.workflows:side_effect_child_workflow",
		"app.test.temporal:test_worker",
		{ message = "Side Effect", count = 2 }
	)

	assert.is_nil(err, "side effect child workflow exec should not error")
	assert.is_table(result, "result table")
	assert.eq(result.status, "completed", "status completed")
	assert.is_table(result.crypto, "crypto child result")
	assert.ok(result.crypto.decrypt_matches, "crypto child decrypt matches")
	assert.is_table(result.uuids, "uuid child result")
	assert.eq(result.uuids.count, 2, "uuid child count")
	assert.eq(#result.uuids.uuids, 2, "uuid list length")

	return true
end

return { main = main }
