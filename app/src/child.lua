-- child.lua
local time = require("time")
local json = require("json")

local function run()
    local info = process.info()
    local args = process.input_args()[1] -- Get the args table we passed
    print(string.format("Child process started: %s with args: %s",
        process.pid(),
        json.encode(args)))

    -- Trigger error for every 3rd child
    if args.child_number and args.child_number % 3 == 0 then
        print(string.format("Child %d triggering deliberate error", args.child_number))
        error(string.format("Deliberate error from child %d", args.child_number))
    end

    local events_ch = pubsub.subscribe("@pid/events")
    local done = channel.new()
    local start = time.now()
    local is_running = true
    local tick = time.ticker("1ms")
    local tick_ch = tick:channel()
    local ten_seconds = time.parse_duration("8ms")

    return {
        name = args.name,
        runtime = "10s",
        start_time = args.start_time,
        end_time = time.now():format("15:04:05"),
        child_number = args.child_number
    }

    --while is_running do
    --    -- Create cases for select
    --    local cases = {
    --        events_ch:case_receive(),
    --        done:case_receive(),
    --        tick_ch:case_receive()
    --    }
    --
    --    local result = channel.select(cases)
    --
    --    if not result.ok then
    --        break
    --    end
    --
    --    -- Handle each channel case
    --    if result.channel == events_ch and result.value then
    --        local event = result.value
    --        if event.event and event.event.kind == "pid.cancel" then
    --            print("Child process cancelled:", process.pid())
    --            is_running = false
    --        end
    --    elseif result.channel == done then
    --        is_running = false
    --    elseif result.channel == tick_ch and result.value then
    --        -- Get current time and calculate duration
    --        local now = time.now()
    --        local duration = now:sub(start)
    --
    --        -- Compare durations directly
    --        if duration:seconds() >= ten_seconds:seconds() then
    --            print(string.format("Child process %s (#%d) completing after 10s",
    --                process.pid(),
    --                args.child_number))
    --            is_running = false
    --        end
    --    end
    --end
    --
    --tick:stop()
    --done:close()
    --
    --return {
    --    name = args.name,
    --    runtime = "10s",
    --    start_time = args.start_time,
    --    end_time = time.now():format("15:04:05"),
    --    child_number = args.child_number
    --}
end

return {
    run = run
}