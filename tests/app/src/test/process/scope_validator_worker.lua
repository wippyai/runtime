-- Worker that validates scope and actor injection
local security = require("security")

local function main()
-- Check that actor was injected
	local actor = security.actor()
	if not actor then
		error("actor not found in security context")
	end

	-- Validate actor ID
	local id = actor:id()
	if id ~= "scope_test_user" then
		error("actor id mismatch: expected scope_test_user, got " .. tostring(id))
	end

	-- Check that scope was injected
	local scope = security.scope()
	if not scope then
		error("scope not found in security context")
	end

	-- Validate scope has policies method
	local policies = scope:policies()
	if not policies then
		error("scope policies not found")
	end

	return true
end

return { main = main }
