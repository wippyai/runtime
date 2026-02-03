-- Worker that validates actor injection
local security = require("security")

local function main()
-- Check that actor was injected
	local actor = security.actor()
	if not actor then
		error("actor not found in security context")
	end

	-- Validate actor ID
	local id = actor:id()
	if id ~= "test_process_user" then
		error("actor id mismatch: expected test_process_user, got " .. tostring(id))
	end

	-- Validate actor metadata
	local meta = actor:meta()
	if not meta then
		error("actor meta not found")
	end

	if meta.role ~= "admin" then
		error("actor role mismatch: expected admin, got " .. tostring(meta.role))
	end

	if meta.department ~= "engineering" then
		error("actor department mismatch: expected engineering, got " .. tostring(meta.department))
	end

	return true
end

return { main = main }
