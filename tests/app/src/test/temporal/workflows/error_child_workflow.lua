-- Child workflow that returns an error with kind
local time = require("time")

local function main(input)
-- Simulate some work
	time.sleep(100 * time.MILLISECOND)

	-- Return an error with kind
	return nil, errors.new({
		message = "child workflow intentional error",
		kind = errors.NOT_FOUND,
		retryable = false
	})
end

return main
