local http = require("http")
local env = require("env")

local function handler()
    -- Get request context and set up response
    local res = http.response()
    if not res then
        return nil, "Failed to create HTTP response"
    end

    res:set_content_type(http.CONTENT.JSON)

    -- Get all environment variables
    local all_vars = env.get_all()
    if not all_vars then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            error = "Failed to get environment variables"
        })
        return
    end

    -- Create response structure with commonly needed vars highlighted
    local response = {
        important = {
            path = env.get("PATH"),
            home = env.get("HOME"),
            user = env.get("USER"),
            pwd = env.get("PWD"),
            shell = env.get("SHELL"),
            term = env.get("TERM")
        },
        all = all_vars
    }

    -- Send response
    res:set_status(http.STATUS.OK)
    res:write_json(response)
end

-- Export the function
return {
    handler = handler
}
