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

    -- Set up inbox for response
    local inbox = process.inbox()

    -- Send terminate request to manager
    local ok = process.send("process_lifecycle_manager", "terminate_process", {
        from = process.pid(),
        reply_to = process.pid(),
        id = id
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
            error = "Terminate request timeout"
        })
        return
    end

    local response = result.value:payload():data()
    if response.status ~= "ok" then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            error = response.error or "Terminate failed"
        })
        return
    end

    res:set_content_type(http.CONTENT.JSON)
    res:write_json({
        id = response.id,
        parent_pid = response.parent_pid,
        status = "terminating",
        message = "Terminate signal sent. This will immediately kill the parent process " +
                  "and will also kill the linked child process unless trap_links is enabled."
    })
end

return { handler = handler }