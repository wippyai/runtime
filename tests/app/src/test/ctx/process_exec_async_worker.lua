-- SPDX-License-Identifier: MPL-2.0

-- Worker executed with process.exec that starts an async function and awaits the future.
local funcs = require("funcs")

local function main()
	local exec = funcs.new():with_context({
		process_exec_async_id = "pefa-987",
		process_exec_async_called = true,
	})

	local future, async_err = exec:async("app.test.ctx:ctx_reader", { "process_exec_async_id", "process_exec_async_called" })
	if async_err then
		return { ok = false, stage = "async_start", error = tostring(async_err) }
	end

	local payload, ok = future:response():receive()
	if not ok then
		local ferr, has_error = future:error()
		return {
			ok = false,
			stage = "future_receive",
			has_error = has_error,
			error = tostring(ferr),
		}
	end
	if not payload then
		return { ok = false, stage = "future_payload", error = "nil payload" }
	end

	local result = payload:data()
	return {
		ok = result.process_exec_async_id == "pefa-987" and result.process_exec_async_called == true,
		stage = "done",
		process_exec_async_id = result.process_exec_async_id,
		process_exec_async_called = result.process_exec_async_called,
	}
end

return { main = main }
