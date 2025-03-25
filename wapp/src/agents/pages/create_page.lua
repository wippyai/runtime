local http = require("http")
local json = require("json")
local registry = require("registry")

local function handler(params)
    -- Validate required inputs
    if not params.title then
        return {
            success = false,
            error = "Missing required parameter: title"
        }
    end

    if not params.name then
        return {
            success = false,
            error = "Missing required parameter: name"
        }
    end

    if not params.content then
        return {
            success = false,
            error = "Missing required parameter: content"
        }
    end

    -- Set defaults
    local icon = params.icon or "tabler:file-text"

    -- Get current timestamp for unique name
    local timestamp = os.time()
    local page_name = params.name .. "-" .. timestamp

    -- Get the current snapshot
    local snapshot, err = registry.snapshot()
    if not snapshot then
        return {
            success = false,
            error = "Failed to get registry snapshot: " .. (err or "unknown error")
        }
    end

    -- Create a changeset from the snapshot
    local changes = snapshot:changes()

    -- Create a new virtual page entry in the registry
    changes:create({
        id = { ns = "fortress.pages", name = page_name },
        kind = "registry.entry",
        meta = {
            type = "virtual.page",
            name = params.name,
            title = params.title,
            icon = icon,
            content_type = "text/html",
            created_at = timestamp,
            order = 100
        },
        data = {
            source = params.content
        }
    })

    -- Apply changes to create a new version
    local version, err = changes:apply()
    if not version then
        return {
            success = false,
            error = "Failed to apply registry changes: " .. (err or "unknown error")
        }
    end

    -- todo: add notification!
    --process.send("user_hub." .. actor:id(), "pages", true)

    -- Return success response with page details
    return {
        success = true,
        message = "Page created successfully",
        page = {
            id = page_name,
            full_id = "fortress.pages:" .. page_name,
            namespace = "fortress.pages",
            name = params.name,
            title = params.title,
            icon = icon,
            version = version:id()
        }
    }
end

return {
    handler = handler
}
