-- Test: cloudstorage error handling
local assert = require("assert_primitives")

local function main()
	local cloudstorage = require("cloudstorage")
	local fs = require("fs")

	-- Test invalid resource ID
	local storage, err = cloudstorage.get("")
	assert.is_nil(storage, "should not have storage for empty ID")
	assert.not_nil(err, "should have error for empty ID")

	-- Test non-existent resource
	local storage2, err2 = cloudstorage.get("app.test.cloudstorage:nonexistent")
	assert.is_nil(storage2, "should not have storage for non-existent resource")
	assert.not_nil(err2, "should have error for non-existent resource")

	-- Test operations on released storage
	local storage3, err3 = cloudstorage.get("app.test.cloudstorage:minio")
	assert.is_nil(err3, "should get storage without error")
	assert.not_nil(storage3, "should have storage connection")

	-- Get temp volume for download tests
	local vol, verr = fs.get("app:temp")
	assert.is_nil(verr, "should get temp volume")
	assert.not_nil(vol, "should have temp volume")

	storage3:release()

	-- Operations after release should error
	local result, err4 = storage3:list_objects()
	assert.is_nil(result, "should not have result after release")
	assert.not_nil(err4, "should have error after release")

	local file, ferr = vol:open("/error_test.txt", "w")
	assert.is_nil(ferr, "should open temp file")
	local content, err5 = storage3:download_object("test.txt", file)
	file:close()
	vol:remove("/error_test.txt")
	assert.is_nil(content, "should not have content after release")
	assert.not_nil(err5, "should have error for download after release")

	local ok, err6 = storage3:upload_object("test.txt", "content")
	assert.is_nil(ok, "should not have ok after release")
	assert.not_nil(err6, "should have error for upload after release")

	local ok2, err7 = storage3:delete_objects({"test.txt"})
	assert.is_nil(ok2, "should not have ok2 after release")
	assert.not_nil(err7, "should have error for delete after release")

	local url, err8 = storage3:presigned_get_url("test.txt")
	assert.is_nil(url, "should not have url after release")
	assert.not_nil(err8, "should have error for presigned_get_url after release")

	local url2, err9 = storage3:presigned_put_url("test.txt")
	assert.is_nil(url2, "should not have url2 after release")
	assert.not_nil(err9, "should have error for presigned_put_url after release")

	return true
end

return { main = main }
