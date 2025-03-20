local http = require("http")
local security = require("security")
local session_repo = require("session_repo")
local message_repo = require("message_repo")

local function handler()
    local res = http.response()
    local req = http.request()

    if not res or not req then
        return nil, "Failed to get HTTP context"
    end

    -- Security check - ensure user is authenticated
    local actor = security.actor()
    if not actor then
        res:set_status(http.STATUS.UNAUTHORIZED)
        res:write_json({
            success = false,
            error = "Authentication required"
        })
        return
    end

    -- Get session ID from query parameter
    local session_id = req:query("session_id")
    if not session_id or session_id == "" then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            success = false,
            error = "Session ID is required"
        })
        return
    end

    -- Get user ID from the authenticated actor
    local user_id = actor:id()

    -- Verify session belongs to the authenticated user
    local session, err = session_repo.get(session_id)
    if err then
        res:set_status(http.STATUS.NOT_FOUND)
        res:write_json({
            success = false,
            error = err
        })
        return
    end

    if session.user_id ~= user_id then
        res:set_status(http.STATUS.FORBIDDEN)
        res:write_json({
            success = false,
            error = "Access denied"
        })
        return
    end

    -- Get query parameters for pagination
    local limit = tonumber(req:query("limit")) or 50
    local offset = tonumber(req:query("offset")) or 0

    -- Enforce limit constraints
    if limit > 100 then
        limit = 100
    elseif limit < 1 then
        limit = 1
    end

    -- Get messages for this session
    local messages, err = message_repo.list_by_session(session_id, limit, offset)
    if err then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            success = false,
            error = err
        })
        return
    end

    -- Return JSON response
    res:set_content_type(http.CONTENT.JSON)
    res:set_status(http.STATUS.OK)
    res:write_json({
        success = true,
        count = #messages,
        session_id = session_id,
        messages = messages
    })
end

return {
    handler = handler
}