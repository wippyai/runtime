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
    -- This should already be available thanks to the security_firewall middleware
    local actor = security.actor()
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
    if not scope then
        res:set_status(http.STATUS.FORBIDDEN)
        res:write_json({
            success = false,
            error = "Authorization required",
            details = "No authorization scope available"
        })
        return
    end

    -- Get the current endpoint path
    local path = req:path()

    -- Determine the response data based on the endpoint
    local endpoint_data = {}

    if path:find("/admin") then
        endpoint_data = {
            section = "admin",
            description = "This is the administrative dashboard area",
            access_level = "admin",
            system_stats = {
                users = 1254,
                active_sessions = 68,
                cpu_usage = "12%",
                memory_usage = "3.2GB"
            }
        }
    elseif path:find("/profile") then
        endpoint_data = {
            section = "profile",
            description = "This is the user profile area",
            access_level = "user",
            profile_data = {
                username = actor:id(),
                email = actor:meta().email or "unknown@example.com",
                role = actor:meta().role or "user",
                last_login = time.now():format_rfc3339()
            }
        }
    else
        endpoint_data = {
            section = "unknown",
            description = "Generic secure area",
            access_level = "default",
            request_info = {
                path = path,
                method = req:method(),
                timestamp = time.now():format_rfc3339()
            }
        }
    end

    -- Get actor permissions for various resources
    local permissions = {}

    -- Add some example permission checks
    permissions.protected_data = security.can("read", "demo:protected")
    permissions.admin_access = security.can("admin", "system:config")
    permissions.user_profile = security.can("read", "api:user.profile")
    permissions.admin_dashboard = security.can("admin", "api:admin.dashboard")

    -- Build the response
    local response = {
        success = true,
        message = "Access granted to secure endpoint",
        actor = {
            id = actor:id(),
            metadata = actor:meta()
        },
        endpoint = endpoint_data,
        permissions = permissions,
        request = {
            path = path,
            method = req:method(),
            timestamp = time.now():format_rfc3339()
        }
    }

    -- Return secure content for authorized users
    res:set_status(http.STATUS.OK)
    res:write_json(response)
end

return {
    handler = handler
}
