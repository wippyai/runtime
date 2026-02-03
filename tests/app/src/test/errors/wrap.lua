-- Test: errors.wrap preserves error properties
local assert = require("assert2")

local function main()
-- Wrap structured error
	local inner = errors.new({
		message = "inner error",
		kind = errors.INVALID,
		retryable = true,
		details = {code = 42}
	})
	local outer = errors.wrap(inner, "outer context")

	assert.eq(outer:kind(), errors.INVALID, "preserves kind")
	assert.eq(outer:retryable(), true, "preserves retryable")
	assert.eq(outer:message(), "inner error", "preserves message")

	local d = outer:details()
	assert.ok(d, "preserves details")
	assert.eq(d.code, 42, "preserves details values")

	-- Wrap plain string
	local wrapped_str = errors.wrap("string error", "context")
	assert.ok(wrapped_str, "wraps string")
	assert.eq(wrapped_str:message(), "string error", "preserves string message")

	return true
end

return { main = main }
