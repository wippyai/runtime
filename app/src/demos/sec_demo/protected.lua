local http = require("http")
local security = require("security")
local json = require("json")

-- Protected resource handler
local function handler()
    local res = http.response()
    local req = http.request()

    -- Set JSON content type
    res:set_content_type(http.CONTENT.JSON)

    -- Get current actor from security context
    local actor = security.actor()

    -- Check if authenticated
    if not actor then
        res:set_status(http.STATUS.UNAUTHORIZED)
        res:write_json({
            success = false,
            error = "Authentication required",
            details = "This resource requires a valid authentication token"
        })
        return
    end

    -- Define the resource ID for this endpoint
    local resource_id = "demo:protected"

    -- Check if actor has permission to access this resource
    local can_access = security.can("read", resource_id)

    -- Handle unauthorized access
    if not can_access then
        res:set_status(403)
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

    -- Return protected content for authorized users
    res:set_status(http.STATUS.OK)
    res:write_json({
        success = true,
        message = "Access granted to protected resource",
        actor = {
            id = actor:id(),
            metadata = actor:meta()
        },
        data = {
            secret = "This is protected information",
            timestamp = os.time(),
            description = "This endpoint demonstrates token-based authentication and permission checking"
        }
    })
end

return {
    handler = handler
}
