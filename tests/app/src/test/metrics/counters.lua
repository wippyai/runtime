local assert = require("assert_primitives")

local function main()
	local metrics = require("metrics")

	-- counter_inc basic
	local ok, err = metrics.counter_inc("test.counter.basic")
	assert.not_nil(ok, "counter_inc returns result")
	assert.eq(ok, true, "counter_inc returns true")
	assert.is_nil(err, "counter_inc no error")

	-- counter_inc with labels
	ok, err = metrics.counter_inc("test.counter.labels", {
		method = "GET",
		path = "/api/users"
	})
	assert.eq(ok, true, "counter_inc with labels works")
	assert.is_nil(err, "no error with labels")

	-- counter_add
	ok, err = metrics.counter_add("test.counter.add", 5)
	assert.eq(ok, true, "counter_add returns true")
	assert.is_nil(err, "counter_add no error")

	-- counter_add with labels
	ok, err = metrics.counter_add("test.counter.add.labels", 10, {
		batch = "batch-1"
	})
	assert.eq(ok, true, "counter_add with labels works")
	assert.is_nil(err, "no error")

	return true
end

return { main = main }
