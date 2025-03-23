local http = require("http")
local json = require("json")
local registry = require("registry")

local function handler(params)
    -- Validate input
    if not params.id then
        return {
            success = false,
            error = "Missing required parameter: id"
        }
    end

    -- Get a snapshot of the registry
    local snapshot, err = registry.snapshot()
    if not snapshot then
        return {
            success = false,
            error = "Failed to get registry snapshot: " .. (err or "unknown error")
        }
    end

    -- Determine the full ID (handling both short and full ID formats)
    local full_id
    if string.match(params.id, ":") then
        -- Already a full ID in format "namespace:name"
        full_id = params.id
    else
        -- Short ID format, assume fortress.pages namespace
        full_id = "fortress.pages:" .. params.id
    end

    -- Get the page entry from registry
    local entry, err = snapshot:get(full_id)

    if not entry then
        return {
            success = false,
            error = "Page not found: " .. params.id
        }
    end

    -- Verify it's a virtual page
    if not entry.meta or entry.meta.type ~= "virtual.page" then
        return {
            success = false,
            error = "Invalid page type: The specified ID does not correspond to a virtual page"
        }
    end

    -- Handle content source
    local content = ""
    local content_type = (entry.meta.content_type or "text/html")

    if entry.data and entry.data.source then
        content = entry.data.source
    else
        return {
            success = false,
            error = "No content: The page does not have any content defined"
        }
    end

    -- Prepare response with metadata and content
    return {
        success = true,
        page = {
            id = entry.id.name,
            full_id = entry.id, -- todo clarify it!
            name = entry.meta.name or "",
            title = entry.meta.title or "",
            icon = entry.meta.icon or "",
            content_type = content_type,
            content = content
        }
    }
end

return {
    handler = handler
}
