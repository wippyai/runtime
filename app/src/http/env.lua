local http = require("http")
local env = require("env")

local function handler()
    -- Set up response
    local res = http.response()
    if not res then
        return nil, "Failed to create HTTP response"
    end

    -- Set content type and status
    res:set_content_type(http.CONTENT.JSON)
    res:set_status(http.STATUS.OK)

    -- Get all environment variables
    local variables, err = env.get_all()
    if err then
        res:set_status(http.STATUS.INTERNAL_SERVER_ERROR)
        res:write_json({
            error = "Failed to retrieve environment variables",
            message = err,
            timestamp = os.time()
        })
        return res
    end

    -- Count the number of variables
    local count = 0
    for _ in pairs(variables) do
        count = count + 1
    end

    -- Return all environment variables
    res:write_json({
        env = variables,
        message = "Environment variables retrieved successfully",
        timestamp = os.time(),
        count = count
    })
    
    -- Explicitly return the response
    return res
end

-- Export the function
return {
    handler = handler
}
