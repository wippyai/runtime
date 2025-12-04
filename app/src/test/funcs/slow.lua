-- Slow function that sleeps for testing async timing
local time = require("time")

local function main(delay_ms, value)
    delay_ms = delay_ms or 50
    time.sleep(delay_ms)
    return {
        delayed = true,
        delay_ms = delay_ms,
        value = value or "done"
    }
end

return { main = main }
