local http = require("http")
local time = require("time")

-- Main handler function
local function handler()
    -- Get response object
    local res = http.response()
    if not res then
        return nil, "Failed to create HTTP response"
    end

    -- Sleep for 30 seconds using time module
    time.sleep(time.parse_duration("30s"))

    -- Set up response headers
    res:set_content_type(http.CONTENT.TEXT)
    res:set_status(200)
    
    -- Set success message in response body
    res:write("Response after 30 second delay")

    -- Ensure the response is sent
    res:flush()
end

return {
    handler = handler
} 