-- Grandchild worker that validates it inherited security context from parent
-- This worker was spawned with plain process.spawn_monitored (no with_context)
-- It should still have actor/scope inherited from the parent's context
local security = require("security")

local function main()
    -- Check that actor was inherited
    local actor = security.actor()
    if not actor then
        error("grandchild: actor NOT inherited - this is the bug!")
    end

    -- Validate actor ID matches what was set in the test
    local id = actor:id()
    if id ~= "inherit_test_user" then
        error("grandchild: actor id mismatch: expected inherit_test_user, got " .. tostring(id))
    end

    -- Check that scope was inherited
    local scope = security.scope()
    if not scope then
        error("grandchild: scope NOT inherited - this is the bug!")
    end

    return true
end

return { main = main }
