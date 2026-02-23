-- SPDX-License-Identifier: MPL-2.0

-- Test: cloudstorage upload operations
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

	-- Upload a simple string
	local ok, err1 = storage:upload_object("test/hello.txt", "Hello, World!")
	assert.is_nil(err1, "upload should not error")
	assert.eq(ok, true, "upload should return true")

	-- Upload another file
	local content = "This is test content for cloudstorage module.\nLine 2\nLine 3"
	local ok2, err2 = storage:upload_object("test/multiline.txt", content)
	assert.is_nil(err2, "upload multiline should not error")
	assert.eq(ok2, true, "upload multiline should return true")

	-- Upload from fs.File (io.Reader)
	local tmp_path = "/cloudstorage_upload_test.txt"
	local file_content = "Content uploaded from fs.File io.Reader"
	local wok, _ = vol:writefile(tmp_path, file_content)
	assert.ok(wok, "should write test file")

	local rfile, rerr = vol:open(tmp_path, "r")
	assert.is_nil(rerr, "should open file for reading")
	local ok3, err3 = storage:upload_object("test/from-file.txt", rfile)
	rfile:close()
	assert.is_nil(err3, "upload from file should not error")
	assert.eq(ok3, true, "upload from file should return true")

	-- Verify uploaded content by downloading
	local dfile, derr = vol:open("/cloudstorage_upload_verify.txt", "w")
	assert.is_nil(derr, "should open verify file")
	local dok, derr2 = storage:download_object("test/from-file.txt", dfile)
	dfile:close()
	assert.is_nil(derr2, "download verify should not error")
	assert.eq(dok, true, "download verify should return true")

	local verified = vol:readfile("/cloudstorage_upload_verify.txt")
	assert.eq(verified, file_content, "uploaded file content should match")

	-- Cleanup local files
	vol:remove(tmp_path)
	vol:remove("/cloudstorage_upload_verify.txt")

	-- Cleanup remote files
	storage:delete_objects({"test/from-file.txt"})
	storage:release()

	return true
end

return { main = main }
