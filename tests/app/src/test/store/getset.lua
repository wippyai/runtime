-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert_primitives")
local store = require("store")

return function()
	local s = store.get("app.test.store:memory")
	assert.not_nil(s, "store.get should return store object")

	-- Test set string value
	local ok, err = s:set("test:key1", "hello world")
	assert.is_nil(err, "s:set should not return error")
	assert.ok(ok, "s:set should return true")

	-- Test get string value
	local val, err = s:get("test:key1")
	assert.is_nil(err, "s:get should not return error")
	assert.eq(val, "hello world", "s:get should return stored value")

	-- Test set number value
	ok, err = s:set("test:key2", 12345)
	assert.is_nil(err, "s:set number should not return error")

	val, err = s:get("test:key2")
	assert.is_nil(err, "s:get number should not return error")
	assert.eq(val, 12345, "s:get should return stored number")

	-- Test set boolean value
	ok, err = s:set("test:key3", true)
	assert.is_nil(err, "s:set boolean should not return error")

	val, err = s:get("test:key3")
	assert.is_nil(err, "s:get boolean should not return error")
	assert.ok(val, "s:get should return stored boolean")

	-- Test set table value
	local tbl = {name = "test", count = 42}
	ok, err = s:set("test:key4", tbl)
	assert.is_nil(err, "s:set table should not return error")

	val, err = s:get("test:key4")
	assert.is_nil(err, "s:get table should not return error")
	assert.eq(val.name, "test", "table field should match")
	assert.eq(val.count, 42, "table field should match")

	-- Test overwrite value
	ok, err = s:set("test:key1", "updated value")
	assert.is_nil(err, "s:set overwrite should not return error")

	val, err = s:get("test:key1")
	assert.is_nil(err, "s:get after overwrite should not return error")
	assert.eq(val, "updated value", "s:get should return updated value")

	-- Test get non-existent key
	val, err = s:get("test:nonexistent")
	assert.not_nil(err, "s:get nonexistent should return error")
	assert.is_nil(val, "s:get nonexistent should return nil value")

	-- Cleanup
	s:delete("test:key1")
	s:delete("test:key2")
	s:delete("test:key3")
	s:delete("test:key4")
	s:release()
end
