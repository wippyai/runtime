local assert = require("assert_primitives")

return function()
    -- Test store.get returns store object
    local s, err = store.get("app.test.store:memory")
    assert.is_nil(err, "store.get should not return error")
    assert.is_not_nil(s, "store.get should return store object")

    -- Test tostring
    local str = tostring(s)
    assert.equals(str, "store.Store{}", "tostring should return store.Store{}")

    -- Test methods exist
    assert.equals(type(s.get), "function", "s:get should be a method")
    assert.equals(type(s.set), "function", "s:set should be a method")
    assert.equals(type(s.has), "function", "s:has should be a method")
    assert.equals(type(s.delete), "function", "s:delete should be a method")
    assert.equals(type(s.release), "function", "s:release should be a method")

    s:release()
end
