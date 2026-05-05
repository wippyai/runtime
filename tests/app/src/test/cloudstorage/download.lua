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

	-- Range download: fetch only a slice of the object.
	local big_body = "ABCDEFGHIJKLMNOPQRSTUVWXYZ" -- 26 bytes
	storage:upload_object("download-test/range.txt", big_body)

	local rfile, rferr = vol:open("/cloudstorage_range.txt", "w")
	assert.is_nil(rferr, "should open range temp file")
	local rok, rerr = storage:download_object("download-test/range.txt", rfile, { range = "bytes=0-4" })
	rfile:close()
	assert.is_nil(rerr, "range download should not error")
	assert.eq(rok, true, "range download should return true")
	local got_range = vol:readfile("/cloudstorage_range.txt")
	assert.eq(got_range, "ABCDE", "range download should return the requested slice")
	vol:remove("/cloudstorage_range.txt")

	-- Suffix range: last 5 bytes.
	local sfile, _ = vol:open("/cloudstorage_suffix.txt", "w")
	local _, serr = storage:download_object("download-test/range.txt", sfile, { range = "bytes=-5" })
	sfile:close()
	assert.is_nil(serr, "suffix range download should not error")
	local got_suffix = vol:readfile("/cloudstorage_suffix.txt")
	assert.eq(got_suffix, "VWXYZ", "suffix range should return the last 5 bytes")
	vol:remove("/cloudstorage_suffix.txt")

	-- Range combined with conditional: matching if_match + range = success.
	local h = storage:head_object("download-test/range.txt")
	local cfile, _ = vol:open("/cloudstorage_cond_range.txt", "w")
	local _, cerr = storage:download_object("download-test/range.txt", cfile, {
		range = "bytes=10-14",
		if_match = h.etag,
	})
	cfile:close()
	assert.is_nil(cerr, "range + matching if_match should succeed")
	local got_cond = vol:readfile("/cloudstorage_cond_range.txt")
	assert.eq(got_cond, "KLMNO", "conditional range should return the requested slice")
	vol:remove("/cloudstorage_cond_range.txt")

	storage:delete_objects({"download-test/range.txt"})

	-- Cleanup
	storage:delete_objects({"download-test/hello.txt"})
	storage:release()

	return true
end

return { main = main }
