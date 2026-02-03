local assert = require("assert_primitives")
local store = require("store")

return function()
	local s = store.get("app.test.store:memory")
	assert.not_nil(s, "store.get should return store object")

	-- Write test data that persist_read will verify
	local ok, err = s:set("persist:test_key", "persist_value")
	assert.is_nil(err, "s:set should not return error")
	assert.ok(ok, "s:set should return true")

	ok, err = s:set("persist:test_number", 42)
	assert.is_nil(err, "s:set number should not return error")

	ok, err = s:set("persist:test_table", {name = "test", items = {1, 2, 3}})
	assert.is_nil(err, "s:set table should not return error")

	s:release()
end
