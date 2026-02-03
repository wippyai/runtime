local assert = require("assert_primitives")

local function main()
	local time = require("time")

	-- Test parse errors (returns nil, structured error)
	local bad_t, err = time.parse("2006-01-02", "invalid-date")
	assert.is_nil(bad_t, "invalid parse returns nil")
	assert.not_nil(err, "invalid parse returns error")
	assert.eq(err:kind(), errors.INVALID, "parse error kind is INVALID")
	assert.eq(err:retryable(), false, "parse error not retryable")

	-- Test parse_duration errors
	local bad_d, d_err = time.parse_duration("invalid")
	assert.is_nil(bad_d, "invalid duration returns nil")
	assert.not_nil(d_err, "invalid duration returns error")
	assert.eq(d_err:kind(), errors.INVALID, "duration error kind is INVALID")
	assert.eq(d_err:retryable(), false, "duration error not retryable")

	-- Test load_location errors
	local bad_loc, loc_err = time.load_location("Invalid/Location")
	assert.is_nil(bad_loc, "invalid location returns nil")
	assert.not_nil(loc_err, "invalid location returns error")
	assert.eq(loc_err:kind(), errors.NOT_FOUND, "location error kind is NOT_FOUND")
	assert.eq(loc_err:retryable(), false, "location error not retryable")

	-- Test empty location name (invalid input)
	local empty_loc, empty_err = time.load_location("")
	assert.is_nil(empty_loc, "empty location returns nil")
	assert.not_nil(empty_err, "empty location returns error")
	assert.eq(empty_err:kind(), errors.INVALID, "empty location error kind is INVALID")
	assert.eq(empty_err:retryable(), false, "empty location error not retryable")

	return true
end

return { main = main }
