local assert = require("assert_primitives")
local store = require("store")

return function()
    -- Test store.get returns store object
    local s, err = store.get("app.test.store:memory")
    assert.is_nil(err, "store.get should not return error")
    assert.not_nil(s, "store.get should return store object")

    -- Test tostring
    local str = tostring(s)
    assert.eq(str, "store.Store{}", "tostring should return store.Store{}")

    -- Test methods exist
    assert.eq(type(s.get), "function", "s:get should be a method")
    assert.eq(type(s.set), "function", "s:set should be a method")
    assert.eq(type(s.has), "function", "s:has should be a method")
    assert.eq(type(s.delete), "function", "s:delete should be a method")
    assert.eq(type(s.release), "function", "s:release should be a method")

    s:release()
end
