-- SPDX-License-Identifier: MPL-2.0

local funcs = require("funcs")

local function main(return_error)
	local value, err = funcs.call("app.test.funcs:raise_with_kind", errors.INVALID, "bad declaration", false)
	if err then
		if return_error then
			return nil, err
		end
		error(err)
	end
	return value
end

return { main = main }
