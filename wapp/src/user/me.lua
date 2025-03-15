local http = require("http")
local security = require("security")
local json = require("json")
local time = require("time")

-- User details endpoint handler - returns current authenticated user info
local function handler()
    local res = http.response()
    local req = http.request()

    if not req or not res then
        return nil, "Failed to get HTTP context"
    end

    -- Set JSON content type
    res:set_content_type(http.CONTENT.JSON)

    -- Get current actor from security context
    -- This should already be available thanks to the token_auth middleware
    local actor = security.actor()
    if not actor then
        res:set_status(http.STATUS.UNAUTHORIZED)
        res:write_json({
            success = false,
            error = "Authentication required",
            details = "This endpoint requires a valid authentication token"
        })
        return
    end

    -- Get current scope from security context
    local scope = security.scope()
    if not scope then
        res:set_status(http.STATUS.FORBIDDEN)
        res:write_json({
            success = false,
            error = "Authorization required",
            details = "No authorization scope available"
        })
        return
    end

    -- Build the response with user details
    local response = {
        success = true,
        message = "User details retrieved successfully",
        user = {
            id = actor:id(),
            metadata = actor:meta(),
            timestamp = time.now():format_rfc3339()
        }
    }

    -- Return user details to the client
    res:set_status(http.STATUS.OK)
    res:write_json(response)
end

return {
    handler = handler
}
