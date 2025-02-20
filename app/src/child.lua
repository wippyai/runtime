local time = require("time")
local json = require("json")

local function run()
    local info = process.info()
    local args = process.input_args()[1]
    print(string.format("Child process started: %s with args: %s",
        process.pid(),
        json.encode(args)))

    local events_ch = process.events()
    local parent_msgs = process.listen("parent_msgs")
    local done = channel.new()
    local start = time.now()
    local is_running = true
    local tick = time.ticker("1s")
    local tick_ch = tick:channel()
    local ten_seconds = time.parse_duration("2s")

    while is_running do
        local cases = {
            events_ch:case_receive(),
            parent_msgs:case_receive(),
            done:case_receive(),
            tick_ch:case_receive()
        }

        local result = channel.select(cases)

        if not result.ok then
            break
        end

        if result.channel == events_ch and result.value then
            local event = result.value
            if event.event and event.event.kind == "pid.cancel" then
                print("Child process cancelled:", process.pid())
                is_running = false
            end
        elseif result.channel == parent_msgs and result.value then
            -- Just log parent messages without responding
            print(string.format("Child %d received message from parent: %s",
                args.child_number, json.encode(result.value)))
        elseif result.channel == done then
            is_running = false
        elseif result.channel == tick_ch and result.value then
            local now = time.now()
            local duration = now:sub(start)

            -- Send periodic updates to parent
            if args.parent_pid then
                process.send(args.parent_pid, "child_msgs", {
                    from = process.pid(),
                    child_number = args.child_number,
                    uptime = tostring(duration),
                    timestamp = now:format("15:04:05")
                })
            end

            if duration:seconds() >= ten_seconds:seconds() then
                print(string.format("Child process %s (#%d) completing after 10s",
                    process.pid(),
                    args.child_number))
                is_running = false
            end
        end
    end

    tick:stop()
    done:close()

    return {
        name = args.name,
        runtime = "10s",
        start_time = args.start_time,
        end_time = time.now():format("15:04:05"),
        child_number = args.child_number
    }
end

return {
    run = run
}