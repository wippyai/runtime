local json = require("json")
local registry = require("registry")

local function handler(params)
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

    -- Prepare search criteria based on parameters
    local criteria = {}

    -- Add kind filter if provided
    if params.kind then
        criteria[".kind"] = params.kind
    end

    -- Get all entries using the criteria
    local entries, err

    if params.namespace then
        -- If namespace provided, get entries from that namespace
        entries = snapshot:namespace(params.namespace)
    else
        -- Otherwise, get all entries matching criteria
        entries, err = snapshot:entries({ limit = 10000 }) -- Get a large number to search through

        if err then
            return {
                success = false,
                error = "Failed to get registry entries: " .. err
            }
        end
    end

    -- Filter entries if we have kind criteria and used namespace method
    if params.kind and params.namespace then
        local filtered = {}
        for _, entry in ipairs(entries) do
            if entry.kind == params.kind then
                table.insert(filtered, entry)
            end
        end
        entries = filtered
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

    -- Extract only the header information for each entry
    for i = offset + 1, end_index do
        local entry = entries[i]
        local header = {
            name = entry.meta.name or entry.id,
            full_id = entry.id,
            kind = entry.kind,
            comment = entry.meta and entry.meta.comment or nil
        }
        table.insert(paged_entries, header)
    end

    -- Sort by namespace and name
    table.sort(paged_entries, function(a, b)
        if a.namespace == b.namespace then
            return a.name < b.name
        else
            return a.namespace < b.namespace
        end
    end)

    -- Return success response with pagination info
    return {
        success = true,
        headers = paged_entries,
        total = total_count,
        offset = offset,
        limit = limit,
        namespace = params.namespace,
        kind = params.kind,
        has_more = end_index < total_count
    }
end

return {
    handler = handler
}
