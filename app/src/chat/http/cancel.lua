local http = require("http")
local time = require("time")
local encryption = require("encryption")

-- Hardcoded manager name
local MANAGER_NAME = "chat_session_manager"

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

    -- Decrypt and validate session token
    local session_pid, err = encryption.decrypt_session_pid(encrypted_token)
    if err then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            error = err
        })
        return
    end

    -- Set up inbox for response
    local inbox = process.inbox()

    -- Send cancel request to manager
    local ok = process.send(MANAGER_NAME, "cancel_session", {
        from = process.pid(),
        reply_to = process.pid(),
        session_pid = session_pid
    })

    if not ok then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            error = "Failed to contact session manager"
        })
        return
    end

    -- Wait for response with timeout
    local timeout = time.after("5s")
    local result = channel.select({
        inbox:case_receive(),
        timeout:case_receive()
    })

    if result.channel == timeout then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            error = "Session cancellation timeout"
        })
        return
    end

    -- Process response
    local response = result.value:payload():data()

    if response.status == "ok" then
        res:set_content_type(http.CONTENT.JSON)
        res:write_json({
            status = "ok",
            message = "Session cancelled successfully"
        })
    else
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            error = response.error or "Failed to cancel session"
        })
    end
end

return { handler = handler }