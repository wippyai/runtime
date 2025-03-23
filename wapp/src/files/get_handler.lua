local http = require("http")
local security = require("security")
local json = require("json")

local file_repo = require("file_repo")

-- Enhanced get file handler that returns file metadata, content, and basic structure
local function get_handler()
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

    -- Get file_id from query parameter first, then try path
    local file_id = req:query("file_id")

    -- If not in query, try to extract from path segment
    if not file_id or file_id == "" then
        local path = req:path()
        file_id = path:match("/files/([^/]+)$")
    end

    if not file_id or file_id == "" then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            success = false,
            error = "File ID is required"
        })
        return
    end

    -- Get file information
    local file, err = file_repo.get(file_id)
    if err then
        res:set_status(http.STATUS.NOT_FOUND)
        res:write_json({
            success = false,
            error = "Failed to get file",
            details = err
        })
        return
    end

    -- Check if the file belongs to the current user
    if file.user_id ~= actor:id() then
        res:set_status(http.STATUS.FORBIDDEN)
        res:write_json({
            success = false,
            error = "You do not have permission to access this file"
        })
        return
    end

    -- Get additional file information if file is ready
    local content = nil
    local facts = nil

    if file.status == "ready" then
        -- Always get the markdown content for ready files
        content, err = file_repo.get_content(file_id)
        if err then
            print("Warning: Failed to get file content: " .. err)
        end

        -- Get facts/Q&A history
        local include_facts = req:query("include_facts") == "true"
        if include_facts then
            facts, err = file_repo.get_facts(file_id)
            if err then
                print("Warning: Failed to get document facts: " .. err)
            end
        end
    end

    -- Build response
    local response = {
        success = true,
        file = file
    }

    if content then
        response.content = content
    end

    if facts then
        response.facts = facts
    end

    -- Return success with file
    res:set_status(http.STATUS.OK)
    res:write_json(response)
end

return {
    get_handler = get_handler
}