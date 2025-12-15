local security = require("security")

local function main(expected_actor_id, expected_meta)
    local result = {}

    -- Get current actor
    local actor = security.actor()
    if actor then
        result.has_actor = true
        result.actor_id = actor:id()
        result.actor_meta = actor:meta()
    else
        result.has_actor = false
    end

    -- Get current scope
    local scope = security.scope()
    if scope then
        result.has_scope = true
        result.policies = scope:policies()
    else
        result.has_scope = false
    end

    -- Test can() if we have a scope
    if scope then
        result.can_read = security.can("read", "test_resource")
        result.can_write = security.can("write", "test_resource")
        result.can_delete = security.can("delete", "test_resource")
    end

    return result
end

return { main = main }
