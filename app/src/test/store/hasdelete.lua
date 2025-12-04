local assert = require("assert_primitives")

return function()
    local s = store.get("app.test.store:memory")
    assert.is_not_nil(s, "store.get should return store object")

    -- Setup test key
    local ok, err = s:set("test:hasdelete", "test value")
    assert.is_nil(err, "s:set should not return error")

    -- Test has returns true for existing key
    local exists, err = s:has("test:hasdelete")
    assert.is_nil(err, "s:has should not return error")
    assert.is_true(exists, "s:has should return true for existing key")

    -- Test has returns false for non-existent key
    exists, err = s:has("test:nonexistent")
    assert.is_nil(err, "s:has for non-existent should not return error")
    assert.is_false(exists, "s:has should return false for non-existent key")

    -- Test delete existing key
    ok, err = s:delete("test:hasdelete")
    assert.is_nil(err, "s:delete should not return error")
    assert.is_true(ok, "s:delete should return true")

    -- Test has returns false after delete
    exists, err = s:has("test:hasdelete")
    assert.is_nil(err, "s:has after delete should not return error")
    assert.is_false(exists, "s:has should return false after delete")

    -- Test get returns error after delete
    local val, err = s:get("test:hasdelete")
    assert.is_not_nil(err, "s:get after delete should return error")
    assert.is_nil(val, "s:get after delete should return nil")

    -- Test delete non-existent key returns error
    ok, err = s:delete("test:nonexistent")
    assert.is_not_nil(err, "s:delete non-existent should return error")

    s:release()
end
