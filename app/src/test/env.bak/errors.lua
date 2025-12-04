local assert = require("assert_primitives")

local function main()
    -- test get non-existent variable
    local val, err = env.get("app.test.env:composite/NON_EXISTENT_VAR_XYZ")
    assert.eq(val, nil, "non-existent var should return nil")
    assert.ok(err ~= nil, "should return error for non-existent var")

    -- test get from non-existent storage
    val, err = env.get("app.test.env:nonexistent/TEST")
    assert.eq(val, nil, "non-existent storage should return nil")
    assert.ok(err ~= nil, "should return error for non-existent storage")

    -- test invalid key format
    local ok, pcall_err = pcall(function()
        env.get("")
    end)
    assert.eq(ok, false, "empty key should raise error")

    -- test get_all from non-existent storage
    local vars
    vars, err = env.get_all("app.test.env:nonexistent")
    assert.eq(vars, nil, "get_all non-existent should return nil")
    assert.ok(err ~= nil, "get_all should error for non-existent storage")
end

return { main = main }
