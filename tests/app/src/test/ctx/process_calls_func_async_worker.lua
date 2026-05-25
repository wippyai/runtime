-- SPDX-License-Identifier: MPL-2.0

-- Worker that calls a function with funcs.async() from inside a process.
-- This guards the process -> async function -> future response path.
local funcs = require("funcs")

local function main()
	local exec = funcs.new():with_context({
		process_to_func_async_id = "ptfa-654",
		process_async_called = true,
	})

	local future, async_err = exec:async("app.test.ctx:ctx_reader", { "process_to_func_async_id", "process_async_called" })
	if async_err then
		error("funcs async failed to start: " .. tostring(async_err))
	end

	local payload, ok = future:response():receive()
	if not ok then
		local ferr, has_error = future:error()
		if has_error then
			error("future closed with error: " .. tostring(ferr))
		end
		error("future response channel closed without payload")
	end
	if not payload then
		error("future response payload is nil")
	end

	local result = payload:data()
	if result.process_to_func_async_id ~= "ptfa-654" then
		error("process_to_func_async_id not inherited: got " .. tostring(result.process_to_func_async_id))
	end
	if result.process_async_called ~= true then
		error("process_async_called marker not inherited: got " .. tostring(result.process_async_called))
	end

	return true
end

return { main = main }
