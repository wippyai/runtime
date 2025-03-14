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
        res:set_status(500)
        res:write_json({
            error = "Hub not available",
            message = "WebSocket hub process is not running"
        })
        return
    end

    print("lookup ok")

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

    -- do not send anything, else, this initiates relay handler to enbable message forwarding for ws
    res:set_header("X-WS-Relay", config_json)
end

return {
    handler = handler
}
