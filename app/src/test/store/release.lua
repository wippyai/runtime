local assert = require("assert_primitives")

return function()
    local s = store.get("app.test.store:memory")
    assert.is_not_nil(s, "store.get should return store object")

    -- Test tostring before release
    local str = tostring(s)
    assert.equals(str, "store.Store{}", "tostring should return store.Store{}")

    -- Test release returns true
    local ok = s:release()
    assert.is_true(ok, "release should return true")

    -- Test tostring after release
    str = tostring(s)
    assert.equals(str, "store.Store{released}", "tostring after release should show released")

    -- Test multiple releases are safe (idempotent)
    ok = s:release()
    assert.is_true(ok, "second release should also return true")

    ok = s:release()
    assert.is_true(ok, "third release should also return true")
end
