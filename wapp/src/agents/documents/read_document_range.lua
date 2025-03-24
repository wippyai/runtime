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

    -- Apply range parameters
    local start = tonumber(params.start) or 0
    local length = tonumber(params.length) or 10000

    -- Validate range parameters
    if start < 0 then
        start = 0
    end

    if start >= #content then
        return {
            success = false,
            error = "Start position exceeds document size",
            document_size = #content
        }
    end

    -- Extract requested range
    local end_pos = math.min(start + length, #content)
    local content_slice = content:sub(start + 1, end_pos) -- Lua strings are 1-indexed

    -- Return successful response with file info and content range
    return {
        success = true,
        document = {
            file_id = file.file_id,
            filename = file.filename,
            mime_type = file.mime_type,
            size = file.size,
            status = file.status
        },
        content = content_slice,
        range = {
            start = start,
            end = end_pos - 1, -- Convert back to 0-indexed for consistency
            length = #content_slice
        },
        total_size = #content
    }
end

return {
    handler = handler
}