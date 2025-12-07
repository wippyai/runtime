-- Test: cloudstorage upload operations
local assert = require("assert_primitives")

local function main()
    local cloudstorage = require("cloudstorage")

    local storage, err = cloudstorage.get("app.test.cloudstorage:minio")
    assert.is_nil(err, "should get storage without error")
    assert.not_nil(storage, "should have storage connection")

    -- Upload a simple string
    local ok, err1 = storage:upload_object("test/hello.txt", "Hello, World!")
    assert.is_nil(err1, "upload should not error")
    assert.eq(ok, true, "upload should return true")

    -- Upload another file
    local content = "This is test content for cloudstorage module.\nLine 2\nLine 3"
    local ok2, err2 = storage:upload_object("test/multiline.txt", content)
    assert.is_nil(err2, "upload multiline should not error")
    assert.eq(ok2, true, "upload multiline should return true")

    -- Cleanup
    storage:release()

    return true
end

return { main = main }
