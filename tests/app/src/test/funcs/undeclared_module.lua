-- SPDX-License-Identifier: MPL-2.0

-- This function tries to require a module that is NOT declared in its modules list.
-- Keep the name dynamic so the application-wide static declaration check does
-- not reject this intentional negative fixture before the runtime test can run.
-- The scoped runtime require still receives "json" and must deny it.

local function main(args)
-- Attempt to require a module not in the modules list.
	local module_name = "json"
	local json = require(module_name)

	-- If we get here, the restriction didn't work
	return { encoded = json.encode({ test = "value" }) }
end

return { main = main }
