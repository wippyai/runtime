-- Test: Without trap_links (default), process fails when linked process fails
-- This tests the spec requirement: "when a linked process fails,
-- the current process will also fail without receiving an event"

local assert = require("assert2")
local time = require("time")

local function main()
    local events_ch = process.events()

    -- Verify trap_links is false by default
    local opts = process.get_options()
    assert.eq(opts.trap_links, false, "trap_links is false by default")

    -- Spawn a monitored worker that will link to an error process
    -- The worker does NOT set trap_links, so it should FAIL when receiving LINK_DOWN
    local worker_pid, err = process.spawn_monitored(
        "app.test.process:trap_links_default_worker",
        "app:processes"
    )
    assert.is_nil(err, "spawn worker no error")

    -- Wait for worker EXIT event
    local timeout = time.after("3s")
    local result = channel.select {
        events_ch:case_receive(),
        timeout:case_receive(),
    }

    if result.channel == timeout then
        return false, "timeout waiting for worker exit"
    end

    local event = result.value
    assert.eq(event.kind, process.event.EXIT, "got EXIT event")
    assert.eq(event.from, worker_pid, "event from worker")

    -- Worker should have exited with error (linked process failed)
    -- Not with success (which would mean it received LINK_DOWN event)
    if event.result and event.result.error then
        -- Worker failed as expected - this is the correct behavior
        return true
    end

    -- If worker returned success with "LINK_DOWN_RECEIVED", that means trap_links
    -- was incorrectly allowing LINK_DOWN delivery
    local result_value = event.result
    if type(result_value) == "table" then
        result_value = result_value.value
    end

    if result_value == "LINK_DOWN_RECEIVED" then
        return false, "worker received LINK_DOWN when it should have failed (trap_links=false)"
    end

    return false, "unexpected worker result: " .. tostring(result_value)
end

return { main = main }
