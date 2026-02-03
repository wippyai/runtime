-- This function tries to require a module that is NOT declared in its modules list.
-- This should fail at runtime because 'json' is not in the modules list.

local function main(args)
-- Attempt to require a module not in the modules list
	local json = require("json")

	-- If we get here, the restriction didn't work
	return { encoded = json.encode({ test = "value" }) }
end

return { main = main }
