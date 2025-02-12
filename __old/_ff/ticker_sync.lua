local time = require("time")
local json = require("json")
local http = require("http")

function ticker()
    -- Get request context
    local res = http.response()

    -- Start ticker coroutine
    local start_time = time.now()
    local ticks = 0
    local max_ticks = 600 -- 10 minutes * 60 seconds

    while ticks < max_ticks do
        -- Create tick data
        local elapsed = time.now():sub(start_time)
        local data = {
            tick = ticks,
            elapsed = tostring(elapsed),
            -- remaining = max_ticks - ticks
        }

        local packed, err = json.encode(data)
        print(packed)

        -- Write JSON response
        res:write(packed .. "\n")
        res:flush()

        -- Sleep for 1 second
        time.sleep(time.parse_duration("1s"))
        ticks = ticks + 1
    end

    -- Send final message
    local final = {
        status = "completed",
        message = "Ticker finished after 10 minutes"
    }
    res:write(json.encode(final) .. "\n")
    res:flush()
end
