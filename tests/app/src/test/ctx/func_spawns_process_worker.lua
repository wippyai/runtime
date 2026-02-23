-- SPDX-License-Identifier: MPL-2.0

-- Worker spawned by a function to verify context inheritance
-- This worker validates it has context values passed to the parent function
local ctx = require("ctx")

local function main()
-- Check context values that should have been inherited from the function call
	local request_id, err1 = ctx.get("func_to_process_id")
	if err1 or request_id ~= "ftp-789" then
		error("func_to_process_id not inherited: got " .. tostring(request_id))
	end

	local marker, err2 = ctx.get("func_spawned")
	if err2 or marker ~= true then
		error("func_spawned marker not inherited: got " .. tostring(marker))
	end

	return true
end

return { main = main }
