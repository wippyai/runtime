-- SPDX-License-Identifier: MPL-2.0

-- Test: process.exec process can await funcs.async future payloads.
local assert = require("assert2")

local function main()
	local result, err = process.exec("app.test.ctx:process_exec_async_worker", "app:processes")
	assert.is_nil(err, "process.exec no error")
	assert.not_nil(result, "process.exec returned result")
	assert.eq(result.ok, true, "async future payload arrived inside process.exec worker")
	assert.eq(result.stage, "done", "worker completed")
	assert.eq(result.process_exec_async_id, "pefa-987", "context id propagated")
	assert.eq(result.process_exec_async_called, true, "context marker propagated")
	return true
end

return { main = main }
