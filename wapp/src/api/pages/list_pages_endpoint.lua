local http = require("http")
local json = require("json")
local security = require("security")
local registry = require("registry")

local function handler()
    -- Get response object
    local res = http.response()
    local req = http.request()
    if not res or not req then
        return nil, "Failed to get HTTP context"
    end

    -- Find all virtual page entries in the registry
    local all_entries, err = registry.find({
        [".kind"] = "registry.entry",
        ["meta.type"] = "virtual.page"
    })

    if err then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            success = false,
            error = err
        })
        return
    end

    -- Filter metadata and transform entries
    local pages = {}
    for _, entry in ipairs(all_entries) do
        if entry.meta then
            -- Create simplified page object with only necessary metadata
            local page = {
                id = entry.id,
                name = entry.meta.name or "",
                title = entry.meta.title or "",
                icon = entry.meta.icon or "",
                order = entry.meta.order or 0,
            }

            table.insert(pages, page)
        end
    end

    -- Sort by title
    table.sort(pages, function(a, b)
        return a.title < b.title
    end)

    -- Return JSON response
    res:set_content_type(http.CONTENT.JSON)
    res:set_status(http.STATUS.OK)
    res:write_json({
        success = true,
        count = #pages,
        pages = pages
    })
end

return {
    handler = handler
}
