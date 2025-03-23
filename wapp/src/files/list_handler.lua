local http = require("http")
local security = require("security")
local json = require("json")

local file_repo = require("file_repo")

-- List files handler
local function list_handler()
    local req = http.request()
    local res = http.response()

    if not req or not res then
        return nil, "Failed to get HTTP context"
    end

    -- Set JSON content type for response
    res:set_content_type(http.CONTENT.JSON)

    -- Get current user from security context
    local actor = security.actor()
    if not actor then
        res:set_status(http.STATUS.UNAUTHORIZED)
        res:write_json({
            success = false,
            error = "Authentication required"
        })
        return
    end

    -- Get user ID from actor
    local user_id = actor:id()
    if not user_id or user_id == "" then
        res:set_status(http.STATUS.UNAUTHORIZED)
        res:write_json({
            success = false,
            error = "Invalid user ID"
        })
        return
    end

    -- Get query parameters directly
    local limit = tonumber(req:query("limit") or "100")
    local offset = tonumber(req:query("offset") or "0")

    -- Get files for the user
    local files, err = file_repo.list_by_user(user_id, limit, offset)
    if err then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            success = false,
            error = "Failed to list files",
            details = err
        })
        return
    end

    -- Return success with files
    res:set_status(http.STATUS.OK)
    res:write_json({
        success = true,
        files = files
    })
end

return {
    list_handler = list_handler
}
