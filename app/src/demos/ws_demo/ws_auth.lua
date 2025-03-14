local http = require("http")
local json = require("json")

-- WebSocket authentication endpoint
-- This generates the relay header that the Go WebSocket relay middleware uses
-- to establish and route the WebSocket connection

function handler()
    local req = http.request()
    local res = http.response()

    if not req or not res then
        return nil, "Failed to get HTTP context"
    end

    -- We only support GET requests for WebSocket connections
    if req:method() ~= http.METHOD.GET then
        res:set_status(http.STATUS.METHOD_NOT_ALLOWED)
        res:write_json({
            error = "Method not allowed",
            message = "Only GET method is supported for WebSocket connections"
        })
        return
    end

    -- Look up the hub process PID by its registered name
    local hub_pid = process.registry.lookup("ws_hub")
    if not hub_pid then
        res:set_status(http.STATUS.SERVICE_UNAVAILABLE)
        res:write_json({
            error = "Hub not available",
            message = "WebSocket hub process is not running"
        })
        return
    end

    -- Create WS relay configuration
    local relay_config = {
        target_pid = hub_pid,
        message_topic = "ws.message" -- Default message topic for WS messages
    }

    -- Encode relay config as JSON
    local config_json, err = json.encode(relay_config)
    if err then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            error = "Configuration error",
            message = "Failed to encode WebSocket configuration"
        })
        return
    end

    -- Set the special relay header that tells the middleware to upgrade to WebSocket
    res:set_header("X-WS-Relay", config_json)

    -- The response itself doesn't matter as the middleware will intercept and upgrade
    -- But we'll set a fallback for clients that don't support WebSockets
    res:set_status(http.STATUS.OK)
    res:set_content_type(http.CONTENT.TEXT)
    res:write(
    "WebSocket connection should be established. If you see this message, your client doesn't support WebSockets.")
end

return {
    handler = handler
}
