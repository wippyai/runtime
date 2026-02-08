local assert = require("assert2")

type NumList = {number}
type ListHolder = {items: {number} @min_len(1) @max_len(3)}
type StrMap = {[string]: string}
type Id = number | string
type Entry = {
	id: Id,
	tags: {string} @min_len(1),
	meta?: {[string]: string}
}

local function main(): boolean
	local list_ok, list_err = ListHolder:is({items = {1, 2}})
	assert.not_nil(list_ok, "ListHolder valid should pass")
	assert.is_nil(list_err, "ListHolder valid should have nil error")

	local list_empty, list_empty_err = ListHolder:is({items = {}})
	assert.is_nil(list_empty, "ListHolder empty should fail")
	assert.not_nil(list_empty_err, "ListHolder empty should return error")
	assert.error_contains(list_empty_err, "length", "ListHolder min_len should mention length")

	local list_big, list_big_err = ListHolder:is({items = {1, 2, 3, 4}})
	assert.is_nil(list_big, "ListHolder too long should fail")
	assert.not_nil(list_big_err, "ListHolder too long should return error")
	assert.error_contains(list_big_err, "length", "ListHolder max_len should mention length")

	local nums_ok, nums_err = NumList:is({1, 2})
	assert.not_nil(nums_ok, "NumList valid should pass")
	assert.is_nil(nums_err, "NumList valid should have nil error")

	local nums_bad, nums_bad_err = NumList:is({1, "x"})
	assert.is_nil(nums_bad, "NumList wrong element type should fail")
	assert.not_nil(nums_bad_err, "NumList wrong element type should return error")
	assert.error_contains(nums_bad_err, "number", "NumList error should mention number")

	local map_ok, map_err = StrMap:is({a = "x", b = "y"})
	assert.not_nil(map_ok, "StrMap valid should pass")
	assert.is_nil(map_err, "StrMap valid should have nil error")

	local map_bad, map_bad_err = StrMap:is({a = 1})
	assert.is_nil(map_bad, "StrMap wrong value type should fail")
	assert.not_nil(map_bad_err, "StrMap wrong value type should return error")
	assert.error_contains(map_bad_err, "string", "StrMap error should mention string")

	local id_num, id_num_err = Id:is(123)
	assert.not_nil(id_num, "Id number should pass")
	assert.is_nil(id_num_err, "Id number should have nil error")

	local id_str, id_str_err = Id:is("abc")
	assert.not_nil(id_str, "Id string should pass")
	assert.is_nil(id_str_err, "Id string should have nil error")

	local id_bad, id_bad_err = Id:is(true)
	assert.is_nil(id_bad, "Id boolean should fail")
	assert.not_nil(id_bad_err, "Id boolean should return error")

	local entry_ok, entry_err = Entry:is({id = "x", tags = {"a", "b"}, meta = {env = "dev"}})
	assert.not_nil(entry_ok, "Entry valid should pass")
	assert.is_nil(entry_err, "Entry valid should have nil error")

	local entry_bad, entry_bad_err = Entry:is({id = true, tags = {"a"}})
	assert.is_nil(entry_bad, "Entry with invalid id should fail")
	assert.not_nil(entry_bad_err, "Entry with invalid id should return error")
	assert.error_contains(entry_bad_err, "id", "Entry error should mention id")

	return true
end

return { main = main }
