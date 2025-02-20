local time = require("time")
local json = require("json")

local function run()
    -- Get process information at startup
    local info = process.info()
    print("Starting process", process.pid())
    print("Process info:", json.encode(info))

    -- Print any input arguments
    local args = process.input_args()
    if args then
        print("Started with args:", json.encode(args))
    end

    local events_ch = pubsub.subscribe("@pid/events")
    local done = channel.new()
    local is_running = true
    local tick = time.ticker("1s")
    local tick_ch = tick:channel()
    local child_count = 0

    while is_running do
        local result = channel.select({
            events_ch:case_receive(),
            done:case_receive(),
            tick_ch:case_receive()
        })

        if not result.ok then
            break
        end

        if result.channel == done then
            break
        end

        if result.channel == tick_ch then
            child_count = child_count + 1
            local child_name = string.format("child_%d", child_count)

            -- Spawn a monitored child process
            local child_pid = process.spawn_monitored(
                "supervisor:child", -- make sure this matches the registry ID
                "system:heap",      -- using same host as parent
                {
                    name = child_name,
                    start_time = time.now():format("15:04:05")
                }
            )

            print(string.format("Parent spawned child process %s at %s",
                child_pid,
                time.now():format("15:04:05")))
        else
            local event = result.value
            print("Parent received event:", json.encode(event))

            if event.event.kind == "pid.cancel" then
                print("Parent process exiting:", process.pid())
                break
            end

            -- Handle child process completion or failure
            if event.event.kind == "pid.result" then
                print(string.format("Parent got result from child %s:", event.event.from))
                if event.result and event.result.error then
                    print(string.format("Child failed with error: %s",
                        json.encode(event.result.error)))
                elseif event.result and event.result.payload then
                    print(string.format("Child completed with result: %s",
                        json.encode(event.result.payload)))
                end
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
