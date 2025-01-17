---@diagnostic disable: undefined-global

-- ANSI color codes
local COLORS = {
    RED = "\27[31m",
    GREEN = "\27[32m",
    YELLOW = "\27[33m",
    RESET = "\27[0m"
}

-- Create a ticker coroutine with specific interval and color
local function create_ticker(chan, interval, name, color)
    return function()
        while true do
            chan:send({ name = name, color = color })
            time.sleep(time.parse_duration(interval))
        end
    end
end

function App()
    -- Create channels
    local ticker_channel = channel.new()
    local signal_channel = channel.named("signal") -- Named channel for signals

    -- Spawn three tickers with different intervals
    coroutine.spawn(create_ticker(ticker_channel, "1s", "Fast Ticker", COLORS.RED))
    coroutine.spawn(create_ticker(ticker_channel, "2s", "Medium Ticker", COLORS.GREEN))
    coroutine.spawn(create_ticker(ticker_channel, "3s", "Slow Ticker", COLORS.YELLOW))

    -- Main loop to receive and print ticks
    while true do
        local result = channel.select{
            ticker_channel:case_receive()--,
            -- signal_channel:case_receive()
        }

        if not result.ok then
            print("Channel closed")
            break
        end

        if result.channel == signal_channel then
            print(COLORS.RESET .. "Received signal: " .. result.value)
        else
            -- Print colored message with timestamp using time module
            print(result.value.color ..
                result.value.name .. " ticked at " .. time.now():format("15:04:05") .. COLORS.RESET)
        end
    end
end

return App
