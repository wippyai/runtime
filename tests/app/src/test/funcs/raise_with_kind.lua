-- SPDX-License-Identifier: MPL-2.0

local time = require("time")

local function main(kind, message, retryable, after_yield)
	if after_yield then
		time.sleep(1)
	end
	if kind == "plain" then
		error("Invalid: " .. message)
	end
	error(errors.new({
		message = message,
		kind = kind,
		retryable = retryable,
		details = { field = "target", nested = { code = "required" } },
	}))
end

return { main = main }
