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

    -- Retrieve the session
    local session, err = session_repo.get(session_id) -- pass user id here
    if err then
        res:set_status(http.STATUS.NOT_FOUND)
        res:write_json({
            success = false,
            error = err
        })
        return
    end

    -- Verify the session belongs to this user
    if session.user_id ~= user_id then
        res:set_status(http.STATUS.FORBIDDEN)
        res:write_json({
            success = false,
            error = "Access denied"
        })
        return
    end

    -- Get the latest message
    local latest_message, _ = message_repo.get_latest(session_id)

    -- Count total messages in session
    local message_count, _ = message_repo.count_by_session(session_id)

    -- Prepare the response
    local response = {
        success = true,
        session = session
    }

    -- Add additional information if available
    if latest_message then
        response.latest_message = latest_message
    end

    if message_count then
        response.message_count = message_count
    end

    -- Return JSON response
    res:set_content_type(http.CONTENT.JSON)
    res:set_status(http.STATUS.OK)
    res:write_json(response)
end

return {
    handler = handler
}