local http = require("http")
local time = require("time")

function handler()
    local req = http.request()
    local res = http.response()
    if not req or not res then
        return nil, "Failed to get HTTP context"
    end

    -- Get message from query params (optional)
    local message = req:query("message") or ("Hello from " .. process.pid())

    -- Get topic from query params (optional)
    local topic = req:query("topic") or "message"

    -- Set up inbox to receive response
    --local inbox = process.inbox()

    -- Send message to target process
    --local ok = process.send("{node2@system:processes|app.demos.message_sending:message.process|0x00002}", topic, {
    --    from = process.pid(),
    --    payload = message
    --})
res:set_status(http.STATUS.OK)
        res:write_json({
            success = false,
            error = "OK"
    })
        return
    --if not ok then
    --    res:set_status(http.STATUS.INTERNAL_ERROR)
    --    res:write_json({
    --        success = false,
    --        error = "Failed to send message"
    --    })
    --    return
    --end
    --
    ---- Wait for response with timeout
    --local timeout = time.after("1s")
    --local result = channel.select({
    --    inbox:case_receive(),
    --    timeout:case_receive()
    --})
    --
    --if result.channel == timeout then
    --    res:set_status(http.STATUS.INTERNAL_ERROR)
    --    res:write_json({
    --        success = false,
    --        error = "Response timeout after 1s"
    --    })
    --    return
    --end
    --
    ---- Set up response
    --res:set_content_type(http.CONTENT.JSON)
    --res:set_status(http.STATUS.OK)
    --res:write_json({
    --    success = true,
    --    topic = topic,
    --    message_sent = message,
    --    response = result.value:payload():data()
    --})
end

return {
    handler = handler
}
