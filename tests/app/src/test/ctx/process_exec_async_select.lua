-- SPDX-License-Identifier: MPL-2.0

-- Test: process.exec target can collect multiple concurrent funcs.async
-- results with channel.select without losing the process target.
local assert = require("assert2")

local function main()
	local result, err = process.exec("app.test.ctx:process_exec_async_select_worker", "app:processes")
	assert.is_nil(err, "process.exec no error")
	assert.not_nil(result, "process.exec returned result")
	assert.eq(result.ok, true, "both async future payloads arrived inside process.exec worker")
	assert.eq(result.stage, "done", "worker completed")
	assert.eq(result.got_id, true, "context id propagated")
	assert.eq(result.got_marker, true, "context marker propagated")
	return true
end

return { main = main }
