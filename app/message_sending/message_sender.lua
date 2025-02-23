local http = require("http")
local time = require("time")

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
    local message = req:query("message") or ("Hello from " .. func.pid())

    -- Set up inbox to receive response
    local inbox = func.inbox()

    -- Send message to target process
    local ok = func.send(target_pid, "message", {
        from = func.pid(),
        payload = message
    })

    if not ok then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            success = false,
            error = "Failed to send message"
        })
        return
    end

    -- Wait for response with timeout
    local timeout = time.after("1s")
    local result = channel.select({
        inbox:case_receive(),
        timeout:case_receive()
    })

    if result.channel == timeout then
        res:set_status(http.STATUS.GATEWAY_TIMEOUT)
        res:write_json({
            success = false,
            error = "Response timeout after 1s"
        })
        return
    end

    -- Set up response
    res:set_content_type(http.CONTENT.JSON)
    res:set_status(http.STATUS.OK)
    res:write_json({
        success = true,
        message_sent = message,
        response = result.value.payload
    })
end

return {
    handler = handler
}
