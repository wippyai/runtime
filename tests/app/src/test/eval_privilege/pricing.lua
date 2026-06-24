-- SPDX-License-Identifier: MPL-2.0

-- Privileged DSL helper. It uses funcs internally; eval'd DSL code may call its
-- API but must never reach funcs itself.
local funcs = require("funcs")

local pricing = {}

function pricing.quote(input)
	local res, err = funcs.call("app.test.funcs:echo", input)
	if err then
		error("pricing.quote: funcs.call failed: " .. tostring(err))
	end
	return res.echo
end

return pricing
