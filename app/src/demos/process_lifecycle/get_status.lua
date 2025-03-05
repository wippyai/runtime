local http = require("http")
local time = require("time")

local function handler()
    local req = http.request()
    local res = http.response()
    if not req or not res then
        return nil, "Failed to get HTTP context"
    end

    -- Set up inbox for response
    local inbox = process.inbox()

    -- Send status request to manager
    local ok = process.send("process_lifecycle_manager", "get_processes", {
        from = process.pid(),
        reply_to = process.pid()
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
            error = "Status request timeout"
        })
        return
    end

    local response = result.value:payload():data()
    if response.status ~= "ok" then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            error = response.error or "Status request failed"
        })
        return
    end

    res:set_content_type(http.CONTENT.JSON)
    res:write_json({
        processes = response.processes
    })
end

return { handler = handler }