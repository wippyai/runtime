-- Test: cloudstorage presigned URL operations
local assert = require("assert_primitives")

local function main()
	local cloudstorage = require("cloudstorage")

	local storage, err = cloudstorage.get("app.test.cloudstorage:minio")
	assert.is_nil(err, "should get storage without error")
	assert.not_nil(storage, "should have storage connection")

	-- Upload a test file
	storage:upload_object("presigned-test/file.txt", "presigned content")

	-- Get presigned GET URL
	local url, err1 = storage:presigned_get_url("presigned-test/file.txt")
	assert.is_nil(err1, "presigned_get_url should not error")
	assert.not_nil(url, "should have presigned GET URL")
	assert.eq(type(url), "string", "URL should be string")
	assert.eq(url:match("^https?://") ~= nil, true, "URL should start with http(s)://")

	-- Get presigned GET URL with expiration
	local url2, err2 = storage:presigned_get_url("presigned-test/file.txt", { expiration = 3600 })
	assert.is_nil(err2, "presigned_get_url with expiration should not error")
	assert.not_nil(url2, "should have presigned GET URL with expiration")

	-- Get presigned PUT URL
	local url3, err3 = storage:presigned_put_url("presigned-test/new-file.txt")
	assert.is_nil(err3, "presigned_put_url should not error")
	assert.not_nil(url3, "should have presigned PUT URL")
	assert.eq(type(url3), "string", "PUT URL should be string")

	-- Get presigned PUT URL with options
	local url4, err4 = storage:presigned_put_url("presigned-test/new-file2.txt", {
		expiration = 3600,
		content_type = "text/plain",
		content_length = 1024
	})
	assert.is_nil(err4, "presigned_put_url with options should not error")
	assert.not_nil(url4, "should have presigned PUT URL with options")

	-- Cleanup
	storage:delete_objects({"presigned-test/file.txt"})
	storage:release()

	return true
end

return { main = main }
