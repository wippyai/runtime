-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert2")

type Box<T> = {value: T}
type Pair<A, B> = {a: A, b: B}
type StringBox = Box<string>
type NumberBox = Box<number>
type StringNumberPair = Pair<string, number>
type User = {id: string @min_len(2)}
type UserBox = Box<User>

local function main(): boolean
	local sb_ok, sb_err = StringBox:is({value = "ok"})
	assert.not_nil(sb_ok, "StringBox valid should pass")
	assert.is_nil(sb_err, "StringBox valid should have nil error")

	local sb_bad, sb_bad_err = StringBox:is({value = 1})
	assert.is_nil(sb_bad, "StringBox wrong value type should fail")
	assert.not_nil(sb_bad_err, "StringBox wrong value type should return error")
	assert.error_contains(sb_bad_err, "string", "StringBox error should mention string")

	local nb_ok, nb_err = NumberBox:is({value = 42})
	assert.not_nil(nb_ok, "NumberBox valid should pass")
	assert.is_nil(nb_err, "NumberBox valid should have nil error")

	local nb_bad, nb_bad_err = NumberBox:is({value = "x"})
	assert.is_nil(nb_bad, "NumberBox wrong value type should fail")
	assert.not_nil(nb_bad_err, "NumberBox wrong value type should return error")
	assert.error_contains(nb_bad_err, "number", "NumberBox error should mention number")

	local pair_ok, pair_err = StringNumberPair:is({a = "x", b = 1})
	assert.not_nil(pair_ok, "Pair valid should pass")
	assert.is_nil(pair_err, "Pair valid should have nil error")

	local pair_bad, pair_bad_err = StringNumberPair:is({a = 2, b = "x"})
	assert.is_nil(pair_bad, "Pair wrong value types should fail")
	assert.not_nil(pair_bad_err, "Pair wrong value types should return error")
	assert.error_contains(pair_bad_err, "expected", "Pair error should mention expected")

	local ub_ok, ub_err = UserBox:is({value = {id = "ab"}})
	assert.not_nil(ub_ok, "UserBox valid should pass")
	assert.is_nil(ub_err, "UserBox valid should have nil error")

	local ub_bad, ub_bad_err = UserBox:is({value = {id = "a"}})
	assert.is_nil(ub_bad, "UserBox invalid id should fail")
	assert.not_nil(ub_bad_err, "UserBox invalid id should return error")
	assert.error_contains(ub_bad_err, "length", "UserBox error should mention length")

	return true
end

return { main = main }
