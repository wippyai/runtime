-- Test: Process error propagation through funcs.call
local assert = require("assert2")
local funcs = require("funcs")

local function main()
-- Call process that throws error
	local _, err = funcs.call("app.test.processfunc:error_process")

	-- Error should be propagated
	assert.not_nil(err, "error returned")
	assert.contains(tostring(err), "intentional process error", "error message propagated")

	return true
end

return { main = main }
