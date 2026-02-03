-- Workflow that calls an activity that errors and captures the error metadata
local funcs = require("funcs")

local function main(input)
	local my_pid = process.pid()

	-- Call activity that will error
	local result, err = funcs.call("app.test.temporal:error_activity", {
		error_kind = "NotFound",
		error_message = "resource not found in activity"
	})

	-- Extract error metadata
	local error_kind = nil
	local error_retryable = nil
	local error_message = nil

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
		else
			error_message = tostring(err)
		end
	end

	return {
		pid = my_pid,
		activity_result = result,
		error_kind = error_kind,
		error_retryable = error_retryable,
		error_message = error_message,
		error_type = type(err),
		status = err and "error_captured" or "no_error"
	}
end

return main
