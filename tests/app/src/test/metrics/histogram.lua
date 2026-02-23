-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert_primitives")

local function main()
	local metrics = require("metrics")

	-- histogram basic
	local ok, err = metrics.histogram("test.histogram.basic", 0.123)
	assert.eq(ok, true, "histogram returns true")
	assert.is_nil(err, "histogram no error")

	-- histogram with labels
	ok, err = metrics.histogram("test.histogram.labels", 0.456, {
		endpoint = "/api/users",
		method = "GET"
	})
	assert.eq(ok, true, "histogram with labels works")
	assert.is_nil(err, "no error")

	-- histogram various values
	ok, err = metrics.histogram("test.histogram.values", 0)
	assert.eq(ok, true, "histogram zero value works")

	ok, err = metrics.histogram("test.histogram.values", 1.5)
	assert.eq(ok, true, "histogram float value works")

	ok, err = metrics.histogram("test.histogram.values", 1000)
	assert.eq(ok, true, "histogram large value works")

	return true
end

return { main = main }
