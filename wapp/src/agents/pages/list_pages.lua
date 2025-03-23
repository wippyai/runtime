local http = require("http")
local json = require("json")
local registry = require("registry")

local function handler()
    -- Get a snapshot of the registry
    local snapshot, err = registry.snapshot()
    if not snapshot then
        return {
            success = false,
            error = "Failed to get registry snapshot: " .. (err or "unknown error")
        }
    end

    -- Find all virtual page entries in the fortress.pages namespace
    local all_entries, err = snapshot:find({
        [".kind"] = "registry.entry",
        ["meta.type"] = "virtual.page"
    })

    if err then
        return {
            success = false,
            error = "Failed to find pages: " .. err
        }
    end

    -- Filter metadata and transform entries
    local pages = {}
    for _, entry in ipairs(all_entries) do
        if entry.meta then
            -- Create simplified page object with only necessary metadata
            local page = {
                id = entry.id.name,
                full_id = entry.id,
                name = entry.meta.name or "",
                title = entry.meta.title or "",
                icon = entry.meta.icon or "",
                content_type = entry.meta.content_type or "text/html",
                created_at = entry.meta.created_at or os.time()
            }

            table.insert(pages, page)
        end
    end

    -- Sort by title
    table.sort(pages, function(a, b)
        return a.title < b.title
    end)

    -- Return results
    return {
        success = true,
        count = #pages,
        pages = pages
    }
end

return {
    handler = handler
}
