local assert = require("assert_primitives")
local security = require("security")
local funcs = require("funcs")

local function main()
    -- Test security.can() with injected security context via funcs
    -- Uses empty scope since registry policies aren't accessible

    -- Create test actor
    local actor = security.new_actor("can_test_user", {
        role = "tester"
    })
    assert.not_nil(actor, "actor should be created")

    -- Create empty scope
    local scope = security.new_scope()
    assert.not_nil(scope, "scope should be created")

    -- Call verify_context with actor and scope injected
    local result, err = funcs.new()
        :with_actor(actor)
        :with_scope(scope)
        :call("app.test.security:verify_context")

    assert.is_nil(err, "call should not error: " .. tostring(err))
    assert.not_nil(result, "result should exist")
    assert.eq(result.has_actor, true, "called function should see actor")
    assert.eq(result.has_scope, true, "called function should see scope")

    -- With empty scope, all can() checks should return false
    assert.eq(result.can_read, false, "empty scope: can_read should be false")
    assert.eq(result.can_write, false, "empty scope: can_write should be false")
    assert.eq(result.can_delete, false, "empty scope: can_delete should be false")

    -- Test can() directly (without security context)
    local allowed = security.can("read", "resource")
    assert.eq(type(allowed), "boolean", "can should return boolean")
    -- Without injected context, can() returns false
    assert.eq(allowed, false, "can should return false without security context")

    return true
end

return { main = main }
