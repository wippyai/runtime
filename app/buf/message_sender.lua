local http = require("http")

function handler()
    local req = http.request()
    local res = http.response()
    if not req or not res then
        return nil, "Failed to get HTTP context"
    end

    -- Get target PID from query params
    local target_pid = req:query("pid")
    if not target_pid then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            error = "Missing required 'pid' query parameter"
        })
        return
    end

    -- Get message from query params (optional)
    local message = req:query("message") or "Hello from " .. func.pid()

    -- Send message to target process
    local ok = func.send(target_pid, "message", { message })

    -- Set up response
    res:set_content_type(http.CONTENT.JSON)
    if ok then
        res:set_status(http.STATUS.OK)
        res:write_json({
            success = true,
            from = func.pid(),
            to = target_pid,
            message = message
        })
    else
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            success = false,
            error = "Failed to send message"
        })
    end
end

return {
    handler = handler
}