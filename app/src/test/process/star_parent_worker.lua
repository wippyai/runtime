-- Star topology parent: spawns N children, sends them our PID, they link to us
-- Then we error - all children should receive LINK_DOWN and die
local time = require("time")

local function main()
    local events_ch = process.events()
    if not events_ch then
        return false, "failed to get events channel"
    end

    local inbox_ch = process.inbox()
    if not inbox_ch then
        return false, "failed to get inbox channel"
    end

    local my_pid = process.pid()
    local child_count = 10  -- spawn 10 children
    local children = {}

    -- Spawn all children (monitored so we know when they complete)
    for i = 1, child_count do
        local child_pid, err = process.spawn_monitored("app.test.process:linker_child_worker", "app:processes")
        if err then
            return false, "spawn child " .. i .. " failed: " .. tostring(err)
        end
        children[i] = child_pid

        -- Send our PID so child can link to us
        process.send(child_pid, "link_to", my_pid)
    end

    -- Wait for all children to report ready
    local ready_count = 0
    local timeout = time.after("10s")

    while ready_count < child_count do
        local result = channel.select {
            inbox_ch:case_receive(),
            timeout:case_receive(),
        }

        if result.channel ~= inbox_ch then
            return false, "timeout waiting for children to be ready: " .. ready_count .. "/" .. child_count
        end

        local msg = result.value
        if msg and msg:topic() == "ready" then
            ready_count = ready_count + 1
        end
    end

    -- All children linked to us - now ERROR to trigger LINK_DOWN cascade
    error("INTENTIONAL_ERROR_TO_TRIGGER_LINK_DOWN")
end

return { main = main }
