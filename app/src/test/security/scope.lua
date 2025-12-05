local assert = require("assert_primitives")
local security = require("security")
local funcs = require("funcs")

local function main()
    -- Test scope creation and funcs with_scope without registry lookup

    -- Create a scope (empty since we can't get policies from registry)
    local scope = security.new_scope()
    assert.not_nil(scope, "scope should be created")

    local policies = scope:policies()
    assert.not_nil(policies, "scope should have policies array")
    assert.eq(#policies, 0, "new scope should have 0 policies")

    -- Create an actor for the test
    local actor = security.new_actor("scope_test_user", {
        department = "engineering"
    })
    assert.not_nil(actor, "actor should be created")

    -- Call verify_context with scope and actor injected
    local result, cerr = funcs.new()
        :with_actor(actor)
        :with_scope(scope)
        :call("app.test.security:verify_context")

    assert.is_nil(cerr, "call should not error: " .. tostring(cerr))
    assert.not_nil(result, "result should exist")
    assert.eq(result.has_scope, true, "called function should see scope")
    assert.eq(result.has_actor, true, "called function should see actor")
    assert.eq(result.actor_id, "scope_test_user", "actor id should match")

    -- With empty scope (no policies), all actions should be denied
    assert.eq(result.can_read, false, "can_read should be false with empty scope")
    assert.eq(result.can_write, false, "can_write should be false with empty scope")
    assert.eq(result.can_delete, false, "can_delete should be false with empty scope")

    return true
end

return { main = main }
