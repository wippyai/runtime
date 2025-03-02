local http = require("http")

local function handler()
    -- Get response object
    local res = http.response()
    if not res then
        return nil, "Failed to get HTTP response"
    end

    -- Get current function's PID
    local pid = process.pid()

    -- Set up response
    res:set_content_type(http.CONTENT.TEXT)
    res:set_status(http.STATUS.OK)

    -- Write PID to response
    res:write("Current Function PID: " .. pid .. "\n")
end

return {
    handler = handler
}
