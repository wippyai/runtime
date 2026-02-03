local assert = require("assert_primitives")

local function main()
	local metrics = require("metrics")

	-- All metrics functions should work if collector is available
	-- or return structured error if not

	-- Test that labels can be nil
	local ok, _ = metrics.counter_inc("test.no.labels")
	assert.not_nil(ok, "returns result without labels")

	-- Test with empty table labels
	ok, _ = metrics.counter_inc("test.empty.labels", {})
	assert.not_nil(ok, "returns result with empty labels")

	-- Test non-string label values are ignored (not error)
	ok, _ = metrics.counter_inc("test.mixed.labels", {
		valid = "value",
		number = 123,        -- ignored
		bool_val = true      -- ignored
	})
	assert.not_nil(ok, "non-string label values ignored gracefully")

	return true
end

return { main = main }
