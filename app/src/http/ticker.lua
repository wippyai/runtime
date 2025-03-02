local time = require("time")
local json = require("json")
local http = require("http")

local function handler()
    -- Set up response with proper streaming
    local res = http.response()
    res:set_content_type(http.CONTENT.JSON)
    res:set_transfer(http.TRANSFER.CHUNKED)

    -- Create channel for communication
    local tick_channel = channel.new(1)

    -- Spawn producer coroutine
    coroutine.spawn(function()
        local start_time = time.now()
        local ticks = 0
        local max_ticks = 600 -- 10 minutes * 60 seconds

        while ticks < max_ticks do
            -- Create tick data
            local elapsed = time.now():sub(start_time)
            local data = {
                tick = ticks,
                elapsed = tostring(elapsed),
                remaining_ticks = max_ticks - ticks
            }

            -- Send data through channel
            local ok = tick_channel:send(data)
            if not ok then
                -- Channel send failed
                return
            end

            -- Sleep for 1 second
            time.sleep(time.parse_duration("1s"))

            ticks = ticks + 1
        end

        -- Send final message
        tick_channel:send({
            status = "completed",
            total_ticks = ticks,
            message = "Ticker finished after 10 minutes"
        })

        -- Close channel
        tick_channel:close()
    end)

    -- Set initial response status
    res:set_status(http.STATUS.OK)

    -- Consumer coroutine (main thread)
    while true do
        -- Receive data from channel
        local data, ok = tick_channel:receive()

        if not ok then
            -- Channel closed or error, exit
            break
        end

        -- Write JSON response chunk
        local packed, encode_err = json.encode(data)
        if encode_err then
            res:set_status(http.STATUS.INTERNAL_ERROR)
            res:write_json({
                error = "JSON encoding failed",
                details = encode_err
            })
            break
        end

        -- Send chunk with newline for event separation
        res:write(packed .. "\n")
        res:flush()

        -- Check for error in data and break if found
        if data.error then
            break
        end
    end

    -- Ensure the response is properly terminated
    res:flush()
end

return {
    handler = handler
}
