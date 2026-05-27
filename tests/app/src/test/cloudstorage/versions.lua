-- SPDX-License-Identifier: MPL-2.0

-- Test: cloudstorage list_objects with include_versions against a versioned bucket.
-- Requires the test bucket to have versioning enabled (see tests/docker-compose.yml minio-setup).
local assert = require("assert_primitives")

local function main()
	local cloudstorage = require("cloudstorage")

	local storage, err = cloudstorage.get("app.test.cloudstorage:minio")
	assert.is_nil(err, "should get storage without error")
	assert.not_nil(storage, "should have storage connection")

	local key = "versions-test/object.txt"

	-- Upload twice to create two versions on the same key.
	local _, uerr1 = storage:upload_object(key, "v1 contents")
	assert.is_nil(uerr1, "first upload should succeed")
	local _, uerr2 = storage:upload_object(key, "v2 contents (overwritten)")
	assert.is_nil(uerr2, "second upload should succeed")

	-- list_objects with include_versions should return both versions of the same key.
	local result, lerr = storage:list_objects({
		prefix = "versions-test/",
		include_versions = true,
	})
	assert.is_nil(lerr, "list_objects with include_versions should not error")
	assert.not_nil(result, "should have a result")
	assert.not_nil(result.objects, "should have objects array")

	local seen = {}
	local matched = 0
	for _, obj in ipairs(result.objects) do
		if obj.key == key then
			matched = matched + 1
			assert.eq(type(obj.version_id), "string", "version_id should be a string")
			assert.eq(obj.version_id ~= "", true, "version_id should not be empty")
			assert.is_nil(seen[obj.version_id], "version_id should be unique per object version")
			seen[obj.version_id] = true
		end
	end
	assert.eq(matched >= 2, true, "should observe at least 2 versions of the key")

	-- Cleanup. With versioning enabled this only creates a delete marker; OK
	-- because the test fully drives its own state.
	storage:delete_objects({ key })
	storage:release()

	return true
end

return { main = main }
