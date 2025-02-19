local time = require("time")
local json = require("json")

local function run()
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

        if result.channel == events_ch then
            local event = result.value
            print("got event")
        end

        if result.channel == done then
            break
        end

        if result.channel == tick_ch then
            print("Tick at:", time.now():format("15:04:05"))
        else
            local event = result.value
            print("GOT EVENT")

            if event then
                -- Handle shutdown event
                if event.type == "shutdown" then
                    is_running = false
                    break
                end

                -- Dump event as JSON
                local encoded = json.encode(event)
                print("Received event:", encoded)
            end
        end
    end

    tick:stop()
    done:close()
    events_ch:close()
end

return {
    run = run
}
