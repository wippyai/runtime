-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert_primitives")
local store = require("store")

return function()
	local s = store.get("app.test.store:memory")
	assert.not_nil(s, "store.get should return store object")

	-- Verify data written by persist_write is still there
	local val, err = s:get("persist:test_key")
	assert.is_nil(err, "s:get should not return error")
	assert.eq(val, "persist_value", "string value should persist between calls")

	val, err = s:get("persist:test_number")
	assert.is_nil(err, "s:get number should not return error")
	assert.eq(val, 42, "number value should persist between calls")

	val, err = s:get("persist:test_table")
	assert.is_nil(err, "s:get table should not return error")
	assert.not_nil(val, "table value should persist")
	assert.eq(val.name, "test", "table.name should persist")
	assert.eq(#val.items, 3, "table.items should persist")

	-- Cleanup
	s:delete("persist:test_key")
	s:delete("persist:test_number")
	s:delete("persist:test_table")

	s:release()
end
