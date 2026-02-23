-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")

type Mode = "fast" | "safe"
type Flag = true
type Count = 3
type Mixed = Mode | Count

local function main(): boolean
	local mode_ok, mode_err = Mode:is("fast")
	assert.not_nil(mode_ok, "Mode literal should pass")
	assert.is_nil(mode_err, "Mode literal should have nil error")

	local mode_bad, mode_bad_err = Mode:is("slow")
	assert.is_nil(mode_bad, "Mode invalid literal should fail")
	assert.not_nil(mode_bad_err, "Mode invalid literal should return error")
	assert.error_contains(mode_bad_err, "expected", "Mode error should mention expected")

	local flag_ok, flag_err = Flag:is(true)
	assert.not_nil(flag_ok, "Flag literal true should pass")
	assert.is_nil(flag_err, "Flag literal true should have nil error")

	local flag_bad, flag_bad_err = Flag:is(false)
	assert.is_nil(flag_bad, "Flag literal false should fail")
	assert.not_nil(flag_bad_err, "Flag literal false should return error")
	assert.error_contains(flag_bad_err, "expected", "Flag error should mention expected")

	local count_ok, count_err = Count:is(3)
	assert.not_nil(count_ok, "Count literal should pass")
	assert.is_nil(count_err, "Count literal should have nil error")

	local count_bad, count_bad_err = Count:is(2)
	assert.is_nil(count_bad, "Count literal mismatch should fail")
	assert.not_nil(count_bad_err, "Count literal mismatch should return error")
	assert.error_contains(count_bad_err, "expected", "Count error should mention expected")

	local mixed_ok1, mixed_err1 = Mixed:is("safe")
	assert.not_nil(mixed_ok1, "Mixed literal string should pass")
	assert.is_nil(mixed_err1, "Mixed literal string should have nil error")

	local mixed_ok2, mixed_err2 = Mixed:is(3)
	assert.not_nil(mixed_ok2, "Mixed literal number should pass")
	assert.is_nil(mixed_err2, "Mixed literal number should have nil error")

	local mixed_bad, mixed_bad_err = Mixed:is(4)
	assert.is_nil(mixed_bad, "Mixed literal mismatch should fail")
	assert.not_nil(mixed_bad_err, "Mixed literal mismatch should return error")
	assert.error_contains(mixed_bad_err, "expected", "Mixed error should mention expected")

	return true
end

return { main = main }
