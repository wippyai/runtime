local json = require("json")
local registry = require("registry")

local function handler(params)
    -- Validate input
    if not params.criteria or type(params.criteria) ~= "table" then
        return {
            success = false,
            error = "Missing or invalid required parameter: criteria (must be a table)"
        }
    end

    -- Set defaults for optional parameters
    local limit = params.limit or 100
    local offset = params.offset or 0

    -- Get a snapshot of the registry
    local snapshot, err = registry.snapshot()
    if not snapshot then
        return {
            success = false,
            error = "Failed to get registry snapshot: " .. (err or "unknown error")
        }
    end

    -- Find entries matching the criteria
    local entries, err = snapshot:find(params.criteria)
    if err then
        return {
            success = false,
            error = "Failed to find entries: " .. err
        }
    end

    -- Apply pagination
    local paged_entries = {}
    local total_count = #entries

    -- Validate offset
    if offset >= total_count then
        offset = math.max(0, total_count - 1)
    end

    -- Determine end index
    local end_index = math.min(offset + limit, total_count)

    -- Extract entries for the current page
    for i = offset + 1, end_index do
        local entry = entries[i]
        -- Create a clean representation of the entry
        local result = {
            id = entry.id,
            kind = entry.kind,
            meta = entry.meta or {},
            -- Include a preview of data but not the full content
            data_preview = type(entry.data) == "table" and json.encode(entry.data):sub(1, 100) .. "..." or tostring(entry.data):sub(1, 100) .. "..."
        }
        table.insert(paged_entries, result)
    end

    -- Return success response with pagination info
    return {
        success = true,
        entries = paged_entries,
        total = total_count,
        offset = offset,
        limit = limit,
        has_more = end_index < total_count,
        criteria = params.criteria
    }
end

return {
    handler = handler
}