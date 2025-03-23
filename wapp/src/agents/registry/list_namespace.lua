local json = require("json")
local registry = require("registry")

local function handler(params)
    -- Validate input
    if not params.namespace then
        return {
            success = false,
            error = "Missing required parameter: namespace"
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

    -- Get all entries in the namespace
    local entries = snapshot:namespace(params.namespace)

    -- Check if we got any entries
    if not entries or #entries == 0 then
        return {
            success = true,
            namespace = params.namespace,
            entries = {},
            total = 0,
            message = "No entries found in namespace: " .. params.namespace
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
            id = entry.meta.name or entry.id,
            full_id = entry.id,
            kind = entry.kind,
            meta = entry.meta and {
                type = entry.meta.type,
                name = entry.meta.name,
                title = entry.meta.title
            } or {}
        }
        table.insert(paged_entries, result)
    end

    -- Sort by name
    table.sort(paged_entries, function(a, b)
        return a.id < b.id
    end)

    -- Return success response with pagination info
    return {
        success = true,
        namespace = params.namespace,
        entries = paged_entries,
        total = total_count,
        offset = offset,
        limit = limit,
        has_more = end_index < total_count
    }
end

return {
    handler = handler
}