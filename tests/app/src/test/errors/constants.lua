-- Test: error kind constants are available and correct
local assert = require("assert2")

local function main()
	assert.eq(errors.NOT_FOUND, "NotFound", "NOT_FOUND")
	assert.eq(errors.ALREADY_EXISTS, "AlreadyExists", "ALREADY_EXISTS")
	assert.eq(errors.INVALID, "Invalid", "INVALID")
	assert.eq(errors.PERMISSION_DENIED, "PermissionDenied", "PERMISSION_DENIED")
	assert.eq(errors.UNAVAILABLE, "Unavailable", "UNAVAILABLE")
	assert.eq(errors.INTERNAL, "Internal", "INTERNAL")
	assert.eq(errors.CANCELED, "Canceled", "CANCELED")
	assert.eq(errors.CONFLICT, "Conflict", "CONFLICT")
	assert.eq(errors.TIMEOUT, "Timeout", "TIMEOUT")
	assert.eq(errors.RATE_LIMITED, "RateLimited", "RATE_LIMITED")
	assert.eq(errors.UNKNOWN, "", "UNKNOWN is empty string")
	return true
end

return { main = main }
