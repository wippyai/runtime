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

    -- Send create process request to manager
    local ok = process.send("process_lifecycle_manager", "create_process", {
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
            error = "Process creation timeout"
        })
        return
    end

    local response = result.value:payload():data()
    if response.status ~= "ok" then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            error = response.error or "Process creation failed"
        })
        return
    end

    res:set_content_type(http.CONTENT.JSON)
    res:write_json({
        id = response.id,
        parent_pid = response.parent_pid,
        message = "Parent process created successfully. A linked child process was also started."
    })
end

return { handler = handler }