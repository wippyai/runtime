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
	for _, obj in ipairs(result.objects) do
		if obj.key:match("^list%-test/") then
			found_count = found_count + 1
		end
	end
	assert.eq(found_count >= 3, true, "should find at least 3 files with prefix")

	-- List with max_keys
	local result2, err2 = storage:list_objects({ prefix = "list-test/", max_keys = 2 })
	assert.is_nil(err2, "list with max_keys should not error")
	assert.not_nil(result2, "should have result2")
	assert.eq(#result2.objects <= 2, true, "should respect max_keys limit")

	-- List without options
	local result3, err3 = storage:list_objects()
	assert.is_nil(err3, "list without options should not error")
	assert.not_nil(result3, "should have result3")

	-- Cleanup
	storage:delete_objects({"list-test/file1.txt", "list-test/file2.txt", "list-test/subdir/file3.txt", "other/file4.txt"})
	storage:release()

	return true
end

return { main = main }
