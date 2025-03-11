local http = require("http")
local time = require("time")
local encryption = require("encryption")

-- Hardcoded manager PID for demo
local MANAGER_NAME = "chat_session_manager"

local function handler()
    local res = http.response()
    if not res then
        return nil, "Failed to get HTTP response"
    end

    -- Set up inbox for response
    local inbox = process.inbox()

    -- Send create session request to manager
    local ok = process.send(MANAGER_NAME, "create_session", {
        from = process.pid(),
        reply_to = process.pid()
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
            error = "Session creation timeout"
        })
        return
    end

    -- raw payload access via data, can be forwarded
    local response = result.value:payload():data()
    if response.status ~= "ok" then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            error = response.error or "Session creation failed"
        })
        return
    end

    -- Encrypt and return the session PID
    local encrypted_token, err = encryption.encrypt_session_pid(response.session_pid)
    if err then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            error = "Failed to secure session: " .. err
        })
        return
    end

    res:set_content_type(http.CONTENT.JSON)
    res:write_json({
        session = encrypted_token
    })
end

return { handler = handler }
