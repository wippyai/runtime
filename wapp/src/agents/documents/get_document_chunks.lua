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

    -- Set pagination parameters
    local limit = tonumber(params.limit) or 20
    local offset = tonumber(params.offset) or 0

    -- Validate order_by parameter (prevent SQL injection)
    local valid_orders = {
        ["created_at ASC"] = true,
        ["created_at DESC"] = true
    }

    local order_by = params.order_by or "created_at ASC"
    if not valid_orders[order_by] then
        order_by = "created_at ASC" -- Default to safe value
    end

    -- Get chunks for the file
    local chunks, err = file_repo.get_chunks(params.file_id, limit, offset, order_by)
    if err then
        return {
            success = false,
            error = "Failed to get document chunks",
            details = err
        }
    end

    -- Parse JSON paths for better usability
    for i, chunk in ipairs(chunks) do
        local success, path_obj = pcall(json.decode, chunk.path)
        if success then
            chunk.path_object = path_obj
        end
    end

    -- Return successful response with chunks
    return {
        success = true,
        document = {
            file_id = file.file_id,
            filename = file.filename,
            status = file.status
        },
        chunks = chunks,
        count = #chunks,
        pagination = {
            limit = limit,
            offset = offset,
            order_by = order_by
        }
    }
end

return {
    handler = handler
}
