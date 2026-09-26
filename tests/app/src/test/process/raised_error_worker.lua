-- SPDX-License-Identifier: MPL-2.0

local time = require("time")

local function main(after_yield)
	if after_yield then
		time.sleep(1)
	end
	error(errors.new({
		message = "bad declaration",
		kind = errors.INVALID,
		retryable = false,
		details = { field = "target", nested = { code = "required" } },
	}))
end

return { main = main }
