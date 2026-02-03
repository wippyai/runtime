-- Helper function that returns structured errors for error propagation testing

local function main(kind, message)
	local retryable = (kind == errors.UNAVAILABLE or kind == errors.TIMEOUT)
	return nil, errors.new({
		message = message or "test error",
		kind = kind or errors.INTERNAL,
		retryable = retryable
	})
end

return { main = main }
