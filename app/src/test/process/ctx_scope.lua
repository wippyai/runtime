-- Test: Scope injection through process.with_context spawn
local assert = require("assert2")
local security = require("security")
local time = require("time")

local function main()
    local events_ch = process.events()

    -- Create an actor for the test
    local actor = security.new_actor("scope_test_user", {
        department = "engineering"
    })
    assert.not_nil(actor, "actor should be created")

    -- Create a scope
    local scope = security.new_scope()
    assert.not_nil(scope, "scope should be created")

    local policies = scope:policies()
    assert.not_nil(policies, "scope should have policies array")
    assert.eq(#policies, 0, "new scope should have 0 policies")

    -- Spawn process with actor and scope injected
    local child_pid, err = process.with_context({})
        :with_actor(actor)
        :with_scope(scope)
        :spawn_monitored("app.test.process:scope_validator_worker", "app:processes")

    assert.is_nil(err, "spawn with scope no error")
    assert.not_nil(child_pid, "spawn with scope returns pid")

    -- Wait for worker to exit
    local timeout = time.after("2s")
    local result = channel.select {
        events_ch:case_receive(),
        timeout:case_receive(),
    }

    if result.channel ~= events_ch then
        return false, "timeout waiting for worker exit"
    end

    local event = result.value
    assert.eq(event.kind, process.event.EXIT, "got EXIT event")
    assert.eq(event.from, child_pid, "event from child")
    assert.is_nil(event.error, "worker exited without error (scope was injected correctly)")

    return true
end

return { main = main }
