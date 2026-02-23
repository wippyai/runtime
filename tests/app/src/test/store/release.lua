-- SPDX-License-Identifier: MPL-2.0

local assert = require("assert_primitives")
local store = require("store")

return function()
	local s = store.get("app.test.store:memory")
	assert.not_nil(s, "store.get should return store object")

	-- Test tostring before release
	local str = tostring(s)
	assert.eq(str, "store.Store{}", "tostring should return store.Store{}")

	-- Test release returns true
	local ok = s:release()
	assert.ok(ok, "release should return true")

	-- Test tostring after release
	str = tostring(s)
	assert.eq(str, "store.Store{released}", "tostring after release should show released")

	-- Test multiple releases are safe (idempotent)
	ok = s:release()
	assert.ok(ok, "second release should also return true")

	ok = s:release()
	assert.ok(ok, "third release should also return true")
end
