-- SPDX-License-Identifier: MPL-2.0

-- Test: cloudstorage basic operations
local assert = require("assert_primitives")

local function main()
	local cloudstorage = require("cloudstorage")

	-- Get storage connection
	local storage, err = cloudstorage.get("app.test.cloudstorage:minio")
	assert.is_nil(err, "should get storage without error")
	assert.not_nil(storage, "should have storage connection")

	-- Release connection
	local ok = storage:release()
	assert.eq(ok, true, "release should return true")

	-- Double release should be fine
	local ok2 = storage:release()
	assert.eq(ok2, true, "double release should return true")

	return true
end

return { main = main }
