-- Test: cloudstorage download operations
local assert = require("assert_primitives")

local function main()
    local cloudstorage = require("cloudstorage")

    local storage, err = cloudstorage.get("app.test.cloudstorage:minio")
    assert.is_nil(err, "should get storage without error")
    assert.not_nil(storage, "should have storage connection")

    -- Upload test content
    local test_content = "Hello, this is test content for download!"
    local ok, err1 = storage:upload_object("download-test/hello.txt", test_content)
    assert.is_nil(err1, "upload should not error")
    assert.eq(ok, true, "upload should return true")

    -- Download the content
    local content, err2 = storage:download_object("download-test/hello.txt")
    assert.is_nil(err2, "download should not error")
    assert.eq(content, test_content, "downloaded content should match uploaded")

    -- Download non-existent file
    local content2, err3 = storage:download_object("download-test/nonexistent.txt")
    assert.is_nil(content2, "should not have content for non-existent file")
    assert.not_nil(err3, "should have error for non-existent file")

    -- Cleanup
    storage:delete_objects({"download-test/hello.txt"})
    storage:release()

    return true
end

return { main = main }
