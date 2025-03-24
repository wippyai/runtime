local http = require("http")
local security = require("security")
local json = require("json")
local file_repo = require("file_repo")

local function handler(params)
    -- Get current user from security context
    local actor = security.actor()
    if not actor then
        return {
            success = false,
            error = "Authentication required"
        }
    end

    -- Get user ID from actor
    local user_id = actor:id()
    if not user_id or user_id == "" then
        return {
            success = false,
            error = "Invalid user ID"
        }
    end

    -- Get parameters
    local limit = tonumber(params.limit) or 100
    local offset = tonumber(params.offset) or 0

    -- Get files for the user
    local files, err = file_repo.list_by_user(user_id, limit, offset)
    if err then
        return {
            success = false,
            error = "Failed to list documents",
            details = err
        }
    end

    -- Return success with files
    return {
        success = true,
        count = #files,
        documents = files
    }
end

return {
    handler = handler
}