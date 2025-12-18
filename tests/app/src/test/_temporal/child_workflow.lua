-- Simple child workflow that returns a result
local time = require("time")

local function main(input)
    local my_pid = process.pid()

    -- Process input
    local message = "no message"
    if input and input.message then
        message = input.message
    end

    -- Simulate some work
    time.sleep(100 * time.MILLISECOND)

    return {
        pid = my_pid,
        received = message,
        status = "child done"
    }
end

return main
