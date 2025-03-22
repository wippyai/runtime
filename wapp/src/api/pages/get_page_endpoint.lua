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

    -- Extract path parameters from the request path
    local path = req:path()
    local page_id = string.match(path, "/pages/content/([^/]+)$")

    if not page_id then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            success = false,
            error = "Missing page ID",
            details = "A page ID must be provided in the URL"
        })
        return
    end

    -- Get the page entry from registry
    local entry, err = registry.get(page_id)

    if not entry then
        res:set_status(http.STATUS.NOT_FOUND)
        res:write_json({
            success = false,
            error = "Page not found",
            details = "No page found with ID: " .. page_id
        })
        return
    end
print(json.encode(entry))
    -- Verify it's a virtual page
    if not entry.meta or entry.meta.type ~= "virtual.page" then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            success = false,
            error = "Invalid page type",
            details = "The specified ID does not correspond to a virtual page"
        })
        return
    end

    -- Handle content source - could be inline or from file
    local content = ""
    local content_type = (entry.meta.content_type or "text/html")

    if entry.data.source then
        res:set_status(http.STATUS.OK)
        res:set_content_type(content_type)
        res:write(entry.data.source)
        return
    else
        -- No valid content source
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            success = false,
            error = "No content",
            details = "The page does not have any content defined"
        })
        return
    end

    -- Prepare response
    local response = {
        success = true,
        page = {
            id = entry.id,
            title = entry.meta.title or "",
            path = entry.meta.path or "",
            description = entry.meta.description or "",
            content = content,
            content_type = content_type
        }
    }

    -- Return response
    res:set_content_type(http.CONTENT.JSON)
    res:set_status(http.STATUS.OK)
    res:write_json(response)
end

return {
    handler = handler
}
