-- Tests that LINK_DOWN is NOT sent when parent exits normally
local time = require("time")

local function main()
    local events_ch = process.events()
    if not events_ch then
        return false, "failed to get events channel"
    end

    -- Spawn linked child
    local child_pid, err = process.spawn_linked_monitored("app.test.process:linked_child_worker", "app:processes")
    if err then
        return false, "spawn linked child failed: " .. tostring(err)
    end

    -- Give child time to start
    time.sleep("50ms")

    -- Spawn linked normal worker - when it exits normally, NO LINK_DOWN
    local normal_pid, err2 = process.spawn_linked_monitored("app.test.process:normal_exit_worker", "app:processes")
    if err2 then
        return false, "spawn normal worker failed: " .. tostring(err2)
    end

    -- Wait briefly - we should get EXIT (from monitoring) but NOT LINK_DOWN
    local timeout = time.after("500ms")
    local result = channel.select {
        events_ch:case_receive(),
        timeout:case_receive(),
    }

    if result.channel == events_ch then
        local event = result.value
        if event then
            local topic = event:topic()
            if topic == process.event.LINK_DOWN then
                return false, "got unexpected LINK_DOWN on normal exit"
            end
            if topic == process.event.EXIT then
                -- This is expected - we monitor the normal worker
                return true
            end
            return false, "unexpected event: " .. tostring(topic)
        end
    end

    -- Timeout is also acceptable - means no LINK_DOWN was sent
    return true
end

return { main = main }
