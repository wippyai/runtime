-- SPDX-License-Identifier: MPL-2.0

local funcs = require("funcs")

local function extract_error(err)
	local error_kind = nil
	local error_retryable = nil
	local error_message = nil
	local error_details = nil

	if err then
		if type(err) == "userdata" or type(err) == "table" then
			if err.kind then
				error_kind = err:kind()
			end
			if err.retryable then
				error_retryable = err:retryable()
			end
			if err.message then
				error_message = err:message()
			end
			if err.details then
				error_details = err:details()
			end
		else
			error_message = tostring(err)
		end
	end

	return error_kind, error_retryable, error_message, error_details
end

local function main(input)
	local mode = (input and input.mode) or "runtime"

	local activity = "app.test.temporal.activities:runtime_error_activity"
	if mode == "missing" then
		activity = "app.test.temporal.activities:missing_activity"
	end

	local result, err = funcs
		.new()
		:with_options({
			["temporal.activity.retry_policy"] = {
				maximum_attempts = 1,
			},
		})
		:call(activity, {
			mode = mode,
		})

	local error_kind, error_retryable, error_message, error_details = extract_error(err)

	return {
		mode = mode,
		status = err and "error_captured" or "no_error",
		activity = activity,
		activity_result = result,
		error_kind = error_kind,
		error_retryable = error_retryable,
		error_message = error_message,
		error_details = error_details,
		error_type = type(err),
	}
end

return main
