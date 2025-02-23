local http = require("http")
local base64 = require("base64")
local time = require("time")

function handler()
    local req = http.request()
    local res = http.response()
    if not req or not res then
        return nil, "Failed to get HTTP context"
    end

    -- Get base64 encoded session PID
    local b64_pid = req:query("session")
    if not b64_pid then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            error = "Missing required 'session' parameter"
        })
        return
    end

    -- Get message text
    local message = req:query("message")
    if not message then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            error = "Missing required 'message' parameter"
        })
        return
    end

    -- Decode session PID
    local session_pid = base64.decode(b64_pid)
    print("DEBUG: Sending to session PID:", session_pid)

    -- Set up inbox for responses
    local inbox = func.inbox()
    print("DEBUG: Sender inbox PID:", func.pid())

    -- Send message to session
    local ok = func.send(session_pid, "message", {
        from = func.pid(),
        text = message
    })

    print("DEBUG: Message send status:", ok)

    if not ok then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            error = "Failed to send message to session"
        })
        return
    end

    -- Set up streaming response
    res:set_transfer(http.TRANSFER.CHUNKED)
    res:set_content_type(http.CONTENT.JSON)

    -- Stream responses until completion flag
    while true do
        local timeout = time.after("50s")
        local result = channel.select({
            inbox:case_receive(),
            timeout:case_receive()
        })

        if result.channel == timeout then
            print("DEBUG: Response timeout")
            return
        end

        if result.channel == inbox and result.value then
            local msg = result.value

            if msg.topic == "response" then
                -- Check for completion
                if msg.payload.done and not msg.payload.text then
                    return
                end

                -- Send response chunk if we have text
                if msg.payload.text then
                    res:write_json(msg.payload)
                    res:write("\n")
                    res:flush()
                end
            end
        end
    end
end

return { handler = handler }
