local http = require("http")
local security = require("security")
local json = require("json")
local file_repo = require("file_repo")

local function handler(params)
    -- Validate required parameters
    if not params.file_id or params.file_id == "" then
        return {
            success = false,
            error = "File ID is required"
        }
    end

    -- Get file information first
    local file, err = file_repo.get(params.file_id)
    if err then
        return {
            success = false,
            error = "Failed to get document",
            details = err
        }
    end

    -- Get file content
    local content, err = file_repo.get_content(params.file_id)
    if err then
        return {
            success = false,
            error = "Failed to get document content",
            details = err
        }
    end

    -- Apply max_size limit if specified
    local max_size = tonumber(params.max_size)
    if max_size and max_size > 0 and #content > max_size then
        content = content:sub(1, max_size) .. "\n\n... [Content truncated due to size limits] ..."
    end

    -- Return successful response with file info and content
    return {
        success = true,
        document = {
            file_id = file.file_id,
            filename = file.filename,
            mime_type = file.mime_type,
            size = file.size,
            status = file.status,
            created_at = file.created_at
        },
        content = content,
        content_size = #content,
        total_size = file.size,
        truncated = (max_size and #content > max_size)
    }
end

return {
    handler = handler
}
