local assert = require("assert_primitives")
local security = require("security")

local function main()
    -- Test scope evaluation directly using new_scope and new_actor
    -- This tests the core security evaluation logic without registry lookup

    -- Create test actors
    local user_actor = security.new_actor("test_user", {
        role = "viewer"
    })
    assert.not_nil(user_actor, "should create user actor")
    assert.eq(user_actor:id(), "test_user", "actor id should match")

    local admin_actor = security.new_actor("admin", {
        role = "admin"
    })
    assert.not_nil(admin_actor, "should create admin actor")

    -- Create an empty scope
    local empty_scope = security.new_scope()
    assert.not_nil(empty_scope, "should create empty scope")

    -- Empty scope should have no policies
    local policies = empty_scope:policies()
    assert.eq(#policies, 0, "empty scope should have 0 policies")

    -- Empty scope evaluation (no policies = undefined, not allow or deny)
    local empty_result = empty_scope:evaluate(user_actor, "read", "resource")
    assert.eq(empty_result, "undefined", "empty scope should return undefined")

    -- Test actor metadata
    local user_meta = user_actor:meta()
    assert.not_nil(user_meta, "user actor should have meta")
    assert.eq(user_meta.role, "viewer", "user role should be viewer")

    local admin_meta = admin_actor:meta()
    assert.not_nil(admin_meta, "admin actor should have meta")
    assert.eq(admin_meta.role, "admin", "admin role should be admin")

    return true
end

return { main = main }
