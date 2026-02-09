local assert = require("assert2")
local funcs = require("funcs")

local function main()
	local sleep_one, err_one = funcs.call("app.test.wasm:sleep_component_sleep_ms", 5)
	assert.is_nil(err_one, "sleep_component_sleep_ms should not error")
	assert.eq(type(sleep_one), "number", "sleep_component_sleep_ms result type")
	assert.ok(sleep_one >= 0, "sleep_component_sleep_ms result should be non-negative")

	local sleep_many, err_many = funcs.call("app.test.wasm:sleep_component_work_with_sleep", 2, 5)
	assert.is_nil(err_many, "sleep_component_work_with_sleep should not error")
	assert.eq(type(sleep_many), "number", "sleep_component_work_with_sleep result type")
	assert.ok(sleep_many >= 0, "sleep_component_work_with_sleep result should be non-negative")

	return true
end

return { main = main }
