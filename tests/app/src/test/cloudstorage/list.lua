-- SPDX-License-Identifier: MPL-2.0

-- Test: cloudstorage list_objects operations
local assert = require("assert_primitives")

local function main()
	local cloudstorage = require("cloudstorage")

	local storage, err = cloudstorage.get("app.test.cloudstorage:minio")
	assert.is_nil(err, "should get storage without error")
	assert.not_nil(storage, "should have storage connection")

	-- Upload some test files first
	storage:upload_object("list-test/file1.txt", "content1")
	storage:upload_object("list-test/file2.txt", "content2")
	storage:upload_object("list-test/subdir/file3.txt", "content3")
	storage:upload_object("other/file4.txt", "content4")

	-- List all objects with prefix
	local result, err1 = storage:list_objects({ prefix = "list-test/" })
	assert.is_nil(err1, "list should not error")
	assert.not_nil(result, "should have result")
	assert.not_nil(result.objects, "should have objects array")

	-- Should find at least our test files
	local found_count = 0
	local sample
	for _, obj in ipairs(result.objects) do
		if obj.key:match("^list%-test/") then
			found_count = found_count + 1
			sample = sample or obj
		end
	end
	assert.eq(found_count >= 3, true, "should find at least 3 files with prefix")
	assert.not_nil(sample, "should have captured a sample object")
	assert.eq(type(sample.last_modified), "number", "last_modified should be a unix timestamp")
	assert.eq(sample.last_modified > 0, true, "last_modified should be > 0")
	assert.eq(type(sample.storage_class), "string", "storage_class should be a string")

	-- List with max_keys
	local result2, err2 = storage:list_objects({ prefix = "list-test/", max_keys = 2 })
	assert.is_nil(err2, "list with max_keys should not error")
	assert.not_nil(result2, "should have result2")
	assert.eq(#result2.objects <= 2, true, "should respect max_keys limit")

	-- List without options
	local result3, err3 = storage:list_objects()
	assert.is_nil(err3, "list without options should not error")
	assert.not_nil(result3, "should have result3")

	-- include_owner = true populates owner on each result.
	local owned, oerr = storage:list_objects({ prefix = "list-test/", include_owner = true })
	assert.is_nil(oerr, "list with include_owner should not error")
	assert.not_nil(owned, "should have owned-listing result")
	assert.eq(#owned.objects > 0, true, "owned listing should contain items")
	local first_owned
	for _, obj in ipairs(owned.objects) do
		if obj.key:match("^list%-test/") then
			first_owned = obj
			break
		end
	end
	assert.not_nil(first_owned, "should have a list-test object in owned listing")
	assert.not_nil(first_owned.owner, "owner table should be present when include_owner=true")
	assert.eq(type(first_owned.owner.id), "string", "owner.id should be a string")
	assert.eq(first_owned.owner.id ~= "", true, "owner.id should not be empty")

	-- Special characters in keys: slashes, spaces, plus signs, percent-encoded chars.
	-- All of these need to round-trip through the URL-signing path.
	local special_keys = {
		"list-test/with space/file.txt",
		"list-test/with+plus/file.txt",
		"list-test/with%20encoded/file.txt",
		"list-test/sub/path/with/many/levels.txt",
	}
	for _, k in ipairs(special_keys) do
		local _, e = storage:upload_object(k, "x")
		assert.is_nil(e, "upload should succeed for key: " .. k)
	end

	local listed, lerr = storage:list_objects({ prefix = "list-test/" })
	assert.is_nil(lerr, "list after special-char uploads should not error")
	local seen_keys = {}
	for _, obj in ipairs(listed.objects) do
		seen_keys[obj.key] = true
	end
	for _, k in ipairs(special_keys) do
		assert.eq(seen_keys[k], true, "list should observe special-character key: " .. k)
	end

	-- Empty prefix listing should return zero results.
	local empty_result, eerr = storage:list_objects({ prefix = "this-prefix-does-not-exist/" })
	assert.is_nil(eerr, "empty-prefix list should not error")
	assert.not_nil(empty_result, "empty-prefix list should return a result")
	assert.eq(#empty_result.objects, 0, "empty-prefix list should have no objects")

	-- Cleanup
	storage:delete_objects({"list-test/file1.txt", "list-test/file2.txt", "list-test/subdir/file3.txt", "other/file4.txt"})
	storage:delete_objects(special_keys)
	storage:release()

	return true
end

return { main = main }
