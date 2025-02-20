-- child.lua
local time = require("time")
local json = require("json")

local function run()
    local info = process.info()
    local args = process.input_args()[1] -- Get the args table we passed
    print(string.format("Child process started: %s with args: %s", 
        process.pid(),
        json.encode(args)))

    local events_ch = pubsub.subscribe("@pid/events")
    local done = channel.new()
    local start = time.now()
    local is_running = true
    local tick = time.ticker("1s")
    local tick_ch = tick:channel()
    local ten_seconds = time.parse_duration("10s")

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
            -- Get elapsed duration and compare
            local elapsed = time.now():sub(start)
            if elapsed:after(ten_seconds) then
                print(string.format("Child process %s completing after 10s", process.pid()))
                break
            end
        else
            local event = result.value
            if event.event.kind == "pid.cancel" then
                print("Child process cancelled:", process.pid())
                break
            end
        end
    end

    tick:stop()
    done:close()

    return {
        name = args.name,
        runtime = "10s",
        start_time = args.start_time,
        end_time = time.now():format("15:04:05")
    }
end

return {
    run = run
}