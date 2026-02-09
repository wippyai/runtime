local funcs = require("funcs")

local function main(input)
	input = input or {}

	local future, start_err = funcs.async("app.test.temporal.activities:slow_echo_activity", {
		delay = input.delay or "3s",
		message = input.message or "cancel-me",
	})
	if start_err then
		return {
			status = "start_error",
			error = tostring(start_err),
		}
	end

	local _, cancel_err = future:cancel()
	if cancel_err then
		return {
			status = "cancel_error",
			error = tostring(cancel_err),
		}
	end

	local _, future_err = future:result()

	return {
		status = "canceled",
		canceled = future:is_canceled(),
		future_error = future_err and tostring(future_err) or nil,
	}
end

return main
