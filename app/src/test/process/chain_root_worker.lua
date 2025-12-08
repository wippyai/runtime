-- Chain root: spawns first link in chain, waits for all to be ready, then errors
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

    local chain_depth = 5  -- Create 5-level chain

    -- Spawn first child in chain
    local first_child, err = process.spawn_monitored("app.test.process:chain_worker", "app:processes")
    if err then
        return false, "spawn first child failed: " .. tostring(err)
    end

    -- Send setup to first child
    process.send(first_child, "setup", {
        parent_pid = process.pid(),
        depth = chain_depth,
    })

    -- Wait for all chain links to report ready
    local ready_count = 0
    local expected = chain_depth + 1  -- depth+1 workers in total chain
    local timeout = time.after("10s")

    while ready_count < expected do
        local result = channel.select {
            inbox_ch:case_receive(),
            timeout:case_receive(),
        }

        if result.channel ~= inbox_ch then
            return false, "timeout waiting for chain ready: " .. ready_count .. "/" .. expected
        end

        local msg = result.value
        if msg and msg:topic() == "chain_ready" then
            ready_count = ready_count + 1
        end
    end

    -- All chain workers ready and linked - ERROR to trigger cascade
    error("CHAIN_ROOT_INTENTIONAL_ERROR")
end

return { main = main }
