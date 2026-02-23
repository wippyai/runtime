-- SPDX-License-Identifier: MPL-2.0

local funcs = require("funcs")

local function main(input)
	input = input or {}

	local first, err1 = funcs.call("app.test.temporal.activities:echo_activity", {
		message = input.first_message or "first",
	})
	if err1 then
		return {
			status = "first_error",
			error = tostring(err1),
		}
	end

	local second, err2 = funcs.call("app.test.temporal.activities:process_data", {
		id = input.id or "42",
		name = input.name or "chain",
	})
	if err2 then
		return {
			status = "second_error",
			error = tostring(err2),
		}
	end

	return {
		status = "ok",
		first = first,
		second = second,
	}
end

return main
