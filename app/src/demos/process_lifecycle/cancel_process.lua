local http = require("http")
local time = require("time")

local function handler()
    local req = http.request()
    local res = http.response()
    if not req or not res then
        return nil, "Failed to get HTTP context"
    end

    -- Get process ID from query params
    local id = tonumber(req:query("id"))
    if not id then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            error = "Missing required 'id' parameter"
        })
        return
    end

    -- Get optional deadline
    local deadline = req:query("deadline") or "5s"

    -- Set up inbox for response
    local inbox = process.inbox()

    -- Send cancel request to manager
    local ok = process.send("process_lifecycle_manager", "cancel_process", {
        from = process.pid(),
        reply_to = process.pid(),
        id = id,
        deadline = deadline
    })

    if not ok then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            error = "Failed to contact process manager"
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
            error = "Cancel request timeout"
        })
        return
    end

    local response = result.value:payload():data()
    if response.status ~= "ok" then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            error = response.error or "Cancel failed"
        })
        return
    end

    res:set_content_type(http.CONTENT.JSON)
    res:write_json({
        id = response.id,
        parent_pid = response.parent_pid,
        status = "cancelling",
        deadline = response.deadline,
        message = "Cancel signal sent. Parent will forward cancel to child process."
    })
end

return { handler = handler }