local http = require("http")
local base64 = require("base64")
local time = require("time")

-- Hardcoded manager PID for demo
local MANAGER_PID = "{Antares@system:heap|chat:session.manager.proc|0x00001}"

function handler()
    local res = http.response()
    if not res then
        return nil, "Failed to get HTTP response"
    end

    -- Set up inbox for response
    local inbox = func.inbox()

    -- Send create session request to manager
    local ok = func.send(MANAGER_PID, "create_session", {
        from = func.pid(),
        reply_to = func.pid()
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

    local response = result.value.payload
    if response.status ~= "ok" then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            error = response.error or "Session creation failed"
        })
        return
    end

    -- Return the actual session PID
    local b64_pid = base64.encode(response.session_pid)
    res:set_content_type(http.CONTENT.JSON)
    res:write_json({
        session = b64_pid
    })
end

return { handler = handler }
