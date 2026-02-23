-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert_primitives")

local function main()
	local time = require("time")

	assert.eq(time.utc:string(), "UTC", "utc location")
	assert.ok(#time.localtz:string() > 0, "localtz location")

	local loc, err = time.load_location("America/New_York")
	assert.is_nil(err, "load_location succeeds")
	assert.eq(loc:string(), "America/New_York", "loaded location name")

	local bad_loc, bad_err = time.load_location("Invalid/Location")
	assert.is_nil(bad_loc, "invalid location returns nil")
	assert.not_nil(bad_err, "invalid location returns error")

	local fixed = time.fixed_zone("EST", -5*3600)
	assert.eq(fixed:string(), "EST", "fixed zone name")

	return true
end

return { main = main }
