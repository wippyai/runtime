-- Test: Context inheritance through nested funcs.call
-- Verifies that when function A is called with context, and A calls function B
-- via funcs.new():call() (without with_context), B should still see the context
local assert = require("assert2")
local funcs = require("funcs")

local function main()
-- Call nested_func_caller with context
-- It will internally call ctx_reader without with_context()
	local exec = funcs.new():with_context({
		request_id = "nested-req-456",
		user_id = 99,
		nested_test = true
	})

	local result, err = exec:call("app.test.ctx:nested_func_caller", { "request_id", "user_id", "nested_test" })
	assert.is_nil(err, "nested call no error")
	assert.not_nil(result, "nested call returns result")

	-- These should be visible in the nested call
	assert.eq(result.request_id, "nested-req-456", "nested context request_id passed")
	assert.eq(result.user_id, 99, "nested context user_id passed")
	assert.eq(result.nested_test, true, "nested context nested_test passed")

	return true
end

return { main = main }
