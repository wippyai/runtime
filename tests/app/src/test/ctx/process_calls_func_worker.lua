-- SPDX-License-Identifier: MPL-2.0

-- Worker that calls a function via funcs.call() to verify context inheritance
-- This tests whether context passes from process to function call
local funcs = require("funcs")

local function main()
-- Call ctx_reader without explicit with_context()
-- If context inheritance works, ctx_reader should see our context values
	local result, err = funcs.new():call("app.test.ctx:ctx_reader", { "process_to_func_id", "process_called" })
	if err then
		error("funcs call failed: " .. tostring(err))
	end

	-- Validate the context was inherited
	if result.process_to_func_id ~= "ptf-321" then
		error("process_to_func_id not inherited: got " .. tostring(result.process_to_func_id))
	end

	if result.process_called ~= true then
		error("process_called marker not inherited: got " .. tostring(result.process_called))
	end

	return true
end

return { main = main }
