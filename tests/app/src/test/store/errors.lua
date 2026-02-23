-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert_primitives")
local store = require("store")

return function()
-- Test error for non-existent store
	local s, err = store.get("app.test.store:nonexistent")
	assert.not_nil(err, "store.get for nonexistent should return error")
	assert.is_nil(s, "store.get for nonexistent should return nil")

	-- Test error has methods
	assert.eq(type(err.kind), "function", "error should have kind method")
	assert.eq(type(err.message), "function", "error should have message method")
	assert.eq(type(err.retryable), "function", "error should have retryable method")

	-- Test error is not retryable
	assert.eq(err:retryable(), false, "error should not be retryable")

	-- Test error for empty ID
	s, err = store.get("")
	assert.not_nil(err, "store.get with empty ID should return error")
	assert.eq(err:kind(), "Invalid", "empty ID error should be Invalid")
	assert.eq(err:retryable(), false, "error should not be retryable")

	-- Test store method errors after release
	s = store.get("app.test.store:memory")
	assert.not_nil(s, "store.get should return store object")
	s:release()

	-- Methods should return error after release
	local _, err = s:get("test:key")
	assert.not_nil(err, "s:get after release should return error")
	assert.eq(err:kind(), "Invalid", "released store error should be Invalid")

	local _, err = s:set("test:key", "value")
	assert.not_nil(err, "s:set after release should return error")
	assert.eq(err:kind(), "Invalid", "released store error should be Invalid")

	local _, err = s:has("test:key")
	assert.not_nil(err, "s:has after release should return error")
	assert.eq(err:kind(), "Invalid", "released store error should be Invalid")

	ok, err = s:delete("test:key")
	assert.not_nil(err, "s:delete after release should return error")
	assert.eq(err:kind(), "Invalid", "released store error should be Invalid")
end
