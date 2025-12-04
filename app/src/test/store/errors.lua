local assert = require("assert_primitives")

return function()
    -- Test error for non-existent store
    local s, err = store.get("app.test.store:nonexistent")
    assert.is_not_nil(err, "store.get for nonexistent should return error")
    assert.is_nil(s, "store.get for nonexistent should return nil")

    -- Test error has methods
    assert.equals(type(err.kind), "function", "error should have kind method")
    assert.equals(type(err.message), "function", "error should have message method")
    assert.equals(type(err.retryable), "function", "error should have retryable method")

    -- Test error is not retryable
    assert.is_false(err:retryable(), "error should not be retryable")

    -- Test error for empty ID
    s, err = store.get("")
    assert.is_not_nil(err, "store.get with empty ID should return error")
    assert.equals(err:kind(), errors.INVALID, "empty ID error should be INVALID")
    assert.is_false(err:retryable(), "error should not be retryable")

    -- Test store method errors after release
    s = store.get("app.test.store:memory")
    assert.is_not_nil(s, "store.get should return store object")
    s:release()

    -- Methods should return error after release
    local val, err = s:get("test:key")
    assert.is_not_nil(err, "s:get after release should return error")
    assert.equals(err:kind(), errors.INVALID, "released store error should be INVALID")

    local ok, err = s:set("test:key", "value")
    assert.is_not_nil(err, "s:set after release should return error")
    assert.equals(err:kind(), errors.INVALID, "released store error should be INVALID")

    local exists, err = s:has("test:key")
    assert.is_not_nil(err, "s:has after release should return error")
    assert.equals(err:kind(), errors.INVALID, "released store error should be INVALID")

    ok, err = s:delete("test:key")
    assert.is_not_nil(err, "s:delete after release should return error")
    assert.equals(err:kind(), errors.INVALID, "released store error should be INVALID")
end
