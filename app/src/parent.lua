local time = require("time")
local json = require("json")

local function run()
    local info = process.info()
    local parent_pid = process.pid()
    print("Starting parent process", parent_pid)
    print("Process info:", json.encode(info))

    local args = process.input_args()
    if args then
        print("Started with args:", json.encode(args))
    end

    local events_ch = process.events()
    local child_msgs = process.listen("child_msgs")
    local done = channel.new()
    local is_running = true
    local tick = time.ticker("2s")
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
            print("Parent received event:", json.encode(event))

            if event.event.kind == "pid.cancel" then
                print("Parent process exiting:", process.pid())
                is_running = false
            end

            if event.event.kind == "pid.result" then
                active_children = active_children - 1
                print(string.format("Parent got result from child %s:", event.event.from))
                if event.result and event.result.error then
                    print(string.format("Child failed with error: %s",
                        json.encode(event.result.error)))
                elseif event.result and event.result.payload then
                    print(string.format("Child completed with payload: %s",
                        json.encode(event.result.payload)))
                end
                print(string.format("ACTIVE CHILDREN: %d", active_children))
            end
        elseif result.channel == child_msgs then
            local msg = result.value
            print("Parent received message from child:", json.encode(msg))
            -- No response sent back to child
        elseif result.channel == done then
            is_running = false
        elseif result.channel == tick_ch then
            child_count = child_count + 1
            active_children = active_children + 1
            local child_name = string.format("child_%d", child_count)

            local child_pid = process.spawn_monitored(
                "supervisor:child",
                "system:heap",
                {
                    name = child_name,
                    start_time = time.now():format("15:04:05"),
                    child_number = child_count,
                    parent_pid = parent_pid
                }
            )

            print(string.format("Parent spawned child process %s (#%d) at %s",
                child_pid,
                child_count,
                time.now():format("15:04:05")))
            print(string.format("ACTIVE CHILDREN: %d", active_children))

            -- Send only initial welcome message
            process.send(child_pid, "parent_msgs", {
                from = parent_pid,
                msg = "Welcome new child!",
                timestamp = time.now():format("15:04:05")
            })
        end
    end

    tick:stop()
    done:close()

    return "complete"
end

return {
    run = run
}