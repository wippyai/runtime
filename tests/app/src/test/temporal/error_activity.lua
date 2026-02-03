-- Activity that returns an error with specific kind
local function main(input)
	local error_kind = input and input.error_kind or "Internal"
	local error_message = input and input.error_message or "activity intentional error"

	-- Return nil and error (Lua convention)
	return nil, errors.new({
		message = error_message,
		kind = error_kind,
		retryable = false
	})
end

return main
