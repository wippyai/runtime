local http = require("http")
local security = require("security")
local json = require("json")
local time = require("time")

-- Secure endpoint handler
local function handler()
    local res = http.response()
    local req = http.request()

    if not req or not res then
        return nil, "Failed to get HTTP context"
    end

    -- Set JSON content type
    res:set_content_type(http.CONTENT.JSON)

    -- Get current actor from security context
    local actor = security.actor()

    -- This should not happen with firewall middleware, but as a safety check
    if not actor then
        res:set_status(http.STATUS.UNAUTHORIZED)
        res:write_json({
            success = false,
            error = "Authentication required",
            details = "This resource requires a valid authentication token"
        })
        return
    end

    -- Get current scope from security context
    local scope = security.scope()

    -- Get actor permissions for various resources
    local permissions = {}

    -- Add some example permission checks
    permissions.protected_data = (security.can("read", "demo:protected") == true)
    permissions.admin_access = (security.can("admin", "system:config") == true)
    permissions.write_access = (security.can("write", "document:user_profile") == true)

    -- Build the response
    local response = {
        success = true,
        message = "Access granted to secure endpoint",
        actor = {
            id = actor:id(),
            metadata = actor:meta()
        },
        scope = {
            policy_count = #scope:policies()
        },
        permissions = permissions,
        timestamp = time.now():format_rfc3339()
    }

    -- Return secure content for authorized users
    res:set_status(http.STATUS.OK)
    res:write_json(response)
end

return {
    handler = handler
}
