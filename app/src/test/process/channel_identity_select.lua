-- Test: Channel identity in complex select scenarios
-- This test mimics the actor.lua pattern where channels are stored
-- and compared after select
local assert = require("assert2")
local time = require("time")

local function main()
    -- Store channels like actor.lua does
    local inbox = process.inbox()
    local events = process.events()
    local internal_channel = channel.new(10)

    -- Build select cases like actor.lua
    local select_cases = {
        inbox:case_receive(),
        events:case_receive(),
        internal_channel:case_receive()
    }

    -- Spawn worker that will trigger events
    local worker_pid, err = process.spawn_monitored("app.test.process:instant_exit_worker", "app:processes")
    if err then
        return false, "spawn failed: " .. tostring(err)
    end

    -- Do select
    local timeout = time.after("2s")
    table.insert(select_cases, timeout:case_receive())

    local result = channel.select(select_cases)

    if result.channel == timeout then
        return false, "timeout - no event received"
    end

    -- This is the critical check that fails in actor.lua
    local matched = false
    local matched_name = "unknown"

    if result.channel == inbox then
        matched = true
        matched_name = "inbox"
    elseif result.channel == events then
        matched = true
        matched_name = "events"
    elseif result.channel == internal_channel then
        matched = true
        matched_name = "internal"
    end

    if not matched then
        return false, "result.channel did not match any stored channel reference. " ..
            "result.channel=" .. tostring(result.channel) ..
            " inbox=" .. tostring(inbox) ..
            " events=" .. tostring(events) ..
            " internal=" .. tostring(internal_channel)
    end

    -- We expect events channel since worker exited
    if matched_name ~= "events" then
        return false, "expected events channel, matched: " .. matched_name
    end

    -- Verify the event
    local event = result.value
    if event.kind ~= process.event.EXIT then
        return false, "expected EXIT event, got: " .. tostring(event.kind)
    end

    return true
end

return { main = main }
