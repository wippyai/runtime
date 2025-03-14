local http = require("http")
local security = require("security")
local json = require("json")

local function handler()
    local res = http.response()
    local req = http.request()

    -- Set JSON content type
    res:set_content_type(http.CONTENT.JSON)

    -- Check if user is authenticated
    local actor = security.actor()
    if not actor then
        res:set_status(http.STATUS.UNAUTHORIZED)
        res:write_json({
            success = false,
            error = "Authentication required",
            details = "This endpoint requires a valid token"
        })
        return
    end

    -- Check if actor has required permissions
    local resource_id = "demo:restricted"
    local can_access = security.can("read", resource_id)

    if not can_access then
        res:set_status(http.STATUS.FORBIDDEN)
        res:write_json({
            success = false,
            error = "Access denied",
            details = "You don't have permission to access this resource",
            actor = {
                id = actor:id(),
                metadata = actor:meta()
            },
            permission = {
                action = "read",
                resource = resource_id,
                allowed = false
            }
        })
        return
    end

    -- Return success with some protected data
    res:set_status(http.STATUS.OK)
    res:write_json({
        success = true,
        message = "Access granted to restricted resource",
        actor = {
            id = actor:id(),
            metadata = actor:meta()
        },
        data = {
            secret = "This is confidential information",
            timestamp = os.time()
        }
    })
end

return {
    handler = handler
}