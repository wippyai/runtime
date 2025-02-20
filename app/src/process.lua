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
            print(string.format("Tick at %s for process %s",
                time.now():format("15:04:05"),
                process.pid()))
        else
            local event = result.value
            print("System event:", json.encode(event))

            if event.event.kind == "pid.cancel" then
                print("Exiting process", process.pid())
                break
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