-- Test: Actor injection through process.with_context spawn
local assert = require("assert2")
local security = require("security")
local time = require("time")

local function main()
    local events_ch = process.events()

    -- Create an actor with ID and metadata
    local actor = security.new_actor("test_process_user", {
        role = "admin",
        department = "engineering"
    })
    assert.not_nil(actor, "actor should be created")
    assert.eq(actor:id(), "test_process_user", "actor id should match")

    -- Spawn process with actor injected
    local child_pid, err = process.with_context({})
        :with_actor(actor)
        :spawn_monitored("app.test.process:actor_validator_worker", "app:processes")

    assert.is_nil(err, "spawn with actor no error")
    assert.not_nil(child_pid, "spawn with actor returns pid")

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
    assert.is_nil(event.error, "worker exited without error (actor was injected correctly)")

    return true
end

return { main = main }
