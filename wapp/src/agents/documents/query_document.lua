local http = require("http")
local security = require("security")
local json = require("json")
local file_repo = require("file_repo")
local chunk_query = require("chunk_query")

local function handler(params)
   -- todo: fix security!

    -- Validate required parameters
    if not params.file_id or params.file_id == "" then
        return {
            success = false,
            error = "File ID is required"
        }
    end

    if not params.query or params.query == "" then
        return {
            success = false,
            error = "Query string is required"
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

    -- Check if file is ready for searching
    if file.status ~= "ready" then
        return {
            success = false,
            error = "Document is not ready for searching",
            status = file.status,
            details = "Document must be in 'ready' status to perform semantic search"
        }
    end

    -- Set query parameters
    local limit = tonumber(params.limit) or 5

    -- Query the document chunks
    local results, err = chunk_query.query(params.file_id, params.query, limit)
    if err then
        return {
            success = false,
            error = "Failed to query document",
            details = err
        }
    end

    -- Format results for better readability
    local formatted_results = {}
    for i, chunk in ipairs(results) do
    print(json.encode(chunk))
        local path_info = chunk.path_object or {}

        formatted_results[i] = {
            chunk_id = chunk.chunk_id,
            content = chunk.content,
            section = path_info.title or "Unknown Section",
            section_path = path_info.section_path or "",
            relevance = 1 - (chunk.distance or 0) -- Convert distance to relevance score
        }
    end

    -- Return successful response with query results
    return {
        success = true,
        document = {
            file_id = file.file_id,
            filename = file.filename,
            status = file.status
        },
        query = params.query,
        results = formatted_results,
        count = #formatted_results
    }
end

return {
    handler = handler
}
