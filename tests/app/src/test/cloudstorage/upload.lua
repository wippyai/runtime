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

	-- Zero-byte upload: the empty string should be a valid object.
	local zok, zerr = storage:upload_object("test/empty.txt", "")
	assert.is_nil(zerr, "zero-byte upload should not error")
	assert.eq(zok, true, "zero-byte upload should return true")
	local zhead, _ = storage:head_object("test/empty.txt")
	assert.not_nil(zhead, "head_object on zero-byte upload should succeed")
	assert.eq(zhead.size, 0, "zero-byte object should report size 0")

	-- Overwrite: re-uploading the same key replaces the body and the etag changes.
	local ovk = "test/overwrite.txt"
	storage:upload_object(ovk, "first")
	local h1 = storage:head_object(ovk)
	storage:upload_object(ovk, "second-and-longer")
	local h2 = storage:head_object(ovk)
	assert.eq(h1.size, 5, "first upload size should be 5")
	assert.eq(h2.size, 17, "second upload size should reflect the new body")
	assert.eq(h1.etag ~= h2.etag, true, "overwrite should produce a new etag")

	-- And a download after overwrite returns the latest body.
	local ofile, _ = vol:open("/cloudstorage_overwrite.txt", "w")
	local _, oderr = storage:download_object(ovk, ofile)
	ofile:close()
	assert.is_nil(oderr, "download after overwrite should not error")
	local got = vol:readfile("/cloudstorage_overwrite.txt")
	assert.eq(got, "second-and-longer", "download should return the latest body")
	vol:remove("/cloudstorage_overwrite.txt")

	-- Cleanup local files
	vol:remove(tmp_path)
	vol:remove("/cloudstorage_upload_verify.txt")

	-- Cleanup remote files
	storage:delete_objects({"test/from-file.txt", "test/empty.txt", ovk})
	storage:release()

	return true
end

return { main = main }
