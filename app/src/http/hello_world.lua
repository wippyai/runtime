local http = require("http")

local function handler()
    -- Set up response
    local res = http.response()
    if not res then
        return nil, "Failed to create HTTP response"
    end

    -- Set content type and status
    res:set_content_type(http.CONTENT.JSON)
    res:set_status(http.STATUS.OK)

    -- Write response with hello world message
    res:write_json({
        message = "Hello World",
        timestamp = os.time()
    })
end

-- Export the function
return {
    handler = handler
}