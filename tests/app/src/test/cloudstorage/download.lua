-- SPDX-License-Identifier: MPL-2.0

-- Test: cloudstorage download operations with fs.File
local assert = require("assert_primitives")

local function main()
	local cloudstorage = require("cloudstorage")
	local fs = require("fs")

	local storage, err = cloudstorage.get("app.test.cloudstorage:minio")
	assert.is_nil(err, "should get storage without error")
	assert.not_nil(storage, "should have storage connection")

	-- Get temp volume
	local vol, verr = fs.get("app:temp")
	assert.is_nil(verr, "should get temp volume")
	assert.not_nil(vol, "should have temp volume")

	-- Upload test content
	local test_content = "Hello, this is test content for download!"
	local ok, err1 = storage:upload_object("download-test/hello.txt", test_content)
	assert.is_nil(err1, "upload should not error")
	assert.eq(ok, true, "upload should return true")

	-- Download to fs.File
	local tmp_path = "/cloudstorage_download_test.txt"
	local file, ferr = vol:open(tmp_path, "w")
	assert.is_nil(ferr, "should open file for writing")
	assert.not_nil(file, "should have file handle")

	local download_ok, err2 = storage:download_object("download-test/hello.txt", file)
	assert.is_nil(err2, "download should not error")
	assert.eq(download_ok, true, "download should return true")

	file:close()

	-- Read back and verify content
	local content = vol:readfile(tmp_path)
	assert.eq(content, test_content, "downloaded content should match uploaded")

	-- Clean up local file
	vol:remove(tmp_path)

	-- Download non-existent file
	local file2, ferr2 = vol:open("/cloudstorage_nonexistent.txt", "w")
	assert.is_nil(ferr2, "should open file for writing")
	local content2, err3 = storage:download_object("download-test/nonexistent.txt", file2)
	file2:close()
	vol:remove("/cloudstorage_nonexistent.txt")
	assert.is_nil(content2, "should not have content for non-existent file")
	assert.not_nil(err3, "should have error for non-existent file")
	assert.eq(err3:kind(), "NotFound", "missing key error should map to NotFound kind")

	-- Cleanup
	storage:delete_objects({"download-test/hello.txt"})
	storage:release()

	return true
end

return { main = main }
