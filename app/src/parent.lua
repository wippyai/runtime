local time = require("time")
local json = require("json")

local function run()
    local parent_pid = process.pid()
    print("Starting parent process", parent_pid)

    -- Tables to track active processes
    local active_pids = {}

    -- Monitor coroutine to periodically print active PIDs
    coroutine.spawn(function()
        local tick = time.ticker("5s")
        local tick_ch = tick:channel()

        while true do
            local result = channel.select{
                tick_ch:case_receive()
            }

            if not result.ok then
                break
            end

            -- Print current active processes
            print("\n--- Active PIDs ---")
            local count = 0
            for pid in pairs(active_pids) do
                count = count + 1
                print(pid)
            end
            print("Total:", count)
            print("-----------------")
        end

        tick:stop()
    end)

    local events_ch = process.events()
    local child_msgs = process.listen("child_msgs")
    local done = channel.new()
    local is_running = true
    local tick = time.ticker("100ns")
    local tick_ch = tick:channel()
    local child_count = 0
    local active_children = 0

    while is_running do
        local result = channel.select{
            events_ch:case_receive(),
            child_msgs:case_receive(),
            done:case_receive(),
            tick_ch:case_receive()
        }

        if not result.ok then
            break
        end

        if result.channel == events_ch then
            local event = result.value
            if event.event.kind == "pid.cancel" then
                is_running = false
            end

            if event.event.kind == "pid.result" then
                active_children = active_children - 1
                -- Immediately remove the PID when process completes
                active_pids[event.event.from] = nil
            end
        elseif result.channel == done then
            is_running = false
        elseif result.channel == tick_ch then
            child_count = child_count + 1

            if active_children <= 1 then
                active_children = active_children + 1

                local child_pid = process.spawn_monitored(
                    "supervisor:child",
                    "system:heap",
                    {
                        name = string.format("child_%d", child_count),
                        start_time = time.now():format("15:04:05"),
                        child_number = child_count,
                        parent_pid = parent_pid
                    }
                )

                -- Just store the PID
                active_pids[child_pid] = true

                -- Send welcome message
                process.send(child_pid, "parent_msgs", {
                    from = parent_pid,
                    msg = "Welcome new child!",
                    timestamp = time.now():format("15:04:05")
                })
            end
        end
    end

    tick:stop()
    done:close()

    return "complete"
end

return {
    run = run
}