local http = require("http")

-- Main handler function
local function handler()
    -- Get response object
    local res = http.response()
    if not res then
        return nil, "Failed to create HTTP response"
    end

    -- Set up response headers
    res:set_content_type(http.CONTENT.TEXT)
    res:set_status(500)
    
    -- Set error message in response body
    res:write("Internal Server Error - Testing retry functionality\n")

    -- Ensure the response is sent
    res:flush()

    error("Error Message")
end

return {
    handler = handler
}