-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")

type Predicate = (number) -> boolean
type Mapper = (string) -> number
type Callback = (number, string) -> (boolean, string)

local function is_even(n: number): boolean
	return n % 2 == 0
end

local function main(): boolean
	local pred_ok, pred_err = Predicate:is(is_even)
	assert.not_nil(pred_ok, "Predicate function should pass")
	assert.is_nil(pred_err, "Predicate function should have nil error")

	local pred_bad, pred_bad_err = Predicate:is({})
	assert.is_nil(pred_bad, "Predicate non-function should fail")
	assert.not_nil(pred_bad_err, "Predicate non-function should return error")
	assert.error_contains(pred_bad_err, "function", "Predicate error should mention function")

	local map_ok, map_err = Mapper:is(function(s) return #s end)
	assert.not_nil(map_ok, "Mapper function should pass")
	assert.is_nil(map_err, "Mapper function should have nil error")

	local cb_ok, cb_err = Callback:is(function(a, b) return a > 0, b end)
	assert.not_nil(cb_ok, "Callback function should pass")
	assert.is_nil(cb_err, "Callback function should have nil error")

	local cb_bad, cb_bad_err = Callback:is(123)
	assert.is_nil(cb_bad, "Callback non-function should fail")
	assert.not_nil(cb_bad_err, "Callback non-function should return error")
	assert.error_contains(cb_bad_err, "function", "Callback error should mention function")

	return true
end

return { main = main }
