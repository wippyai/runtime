-- Test: cloudstorage delete operations
local assert = require("assert_primitives")

local function main()
    local cloudstorage = require("cloudstorage")

    local storage, err = cloudstorage.get("app.test.cloudstorage:minio")
    assert.is_nil(err, "should get storage without error")
    assert.not_nil(storage, "should have storage connection")

    -- Upload test files
    storage:upload_object("delete-test/file1.txt", "content1")
    storage:upload_object("delete-test/file2.txt", "content2")
    storage:upload_object("delete-test/file3.txt", "content3")

    -- Delete single file
    local ok, err1 = storage:delete_objects({"delete-test/file1.txt"})
    assert.is_nil(err1, "delete single should not error")
    assert.eq(ok, true, "delete single should return true")

    -- Verify file is deleted
    local content, err2 = storage:download_object("delete-test/file1.txt")
    assert.is_nil(content, "deleted file should not have content")
    assert.not_nil(err2, "deleted file should have error")

    -- Delete multiple files
    local ok2, err3 = storage:delete_objects({"delete-test/file2.txt", "delete-test/file3.txt"})
    assert.is_nil(err3, "delete multiple should not error")
    assert.eq(ok2, true, "delete multiple should return true")

    -- Delete non-existent files (should not error)
    local ok3, err4 = storage:delete_objects({"delete-test/nonexistent.txt"})
    assert.is_nil(err4, "delete non-existent should not error")
    assert.eq(ok3, true, "delete non-existent should return true")

    -- Cleanup
    storage:release()

    return true
end

return { main = main }
