local http = require("http")
local security = require("security")
local session_repo = require("session_repo")

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

    -- Get user ID from the authenticated actor
    local user_id = actor:id()

    -- Get query parameters for pagination
    local limit = tonumber(req:query("limit")) or 20
    local offset = tonumber(req:query("offset")) or 0

    -- Enforce limit constraints
    if limit > 100 then
        limit = 100
    elseif limit < 1 then
        limit = 1
    end

    -- Get sessions for this user
    local sessions, err = session_repo.list_by_user(user_id, limit, offset)
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
        count = #sessions,
        sessions = sessions
    })
end

return {
    handler = handler
}