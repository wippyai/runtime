local http = require("http")
local time = require("time")
local encryption = require("encryption")

local function handler()
    local req = http.request()
    local res = http.response()
    if not req or not res then
        return nil, "Failed to get HTTP context"
    end

    -- Get encrypted session token
    local encrypted_token = req:query("session")
    if not encrypted_token then
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

    -- Decrypt and validate session token
    local session_pid, err = encryption.decrypt_session_pid(encrypted_token)
    if err then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            error = err
        })
        return
    end

    -- Set up inbox for responses
    local inbox = process.inbox()

    -- Send message to session
    local ok = process.send(session_pid, "message", {
        from = process.pid(),
        text = message
    })

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

            if msg:topic() == "response" then
                local data = msg:payload():data()

                -- Check for completion
                if data.done and not data.text then
                    return
                end

                -- Send response chunk if we have text
                if data.text then
                    res:write_json(data)
                    res:write("\n")
                    res:flush()
                end
            end
        end
    end
end

return { handler = handler }
