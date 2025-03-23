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

    -- Create a changeset for updates
    local changes = snapshot:changes()

    -- Create updated entry based on original
    local updated_entry = {
        id = entry.id,
        kind = entry.kind,
        meta = entry.meta,
        data = entry.data
    }

    -- Update fields if provided
    if params.title then
        updated_entry.meta.title = params.title
    end

    if params.icon then
        updated_entry.meta.icon = params.icon
    end

    if params.content then
        updated_entry.data.source = params.content
    end

    -- Set updated timestamp
    updated_entry.meta.updated_at = os.time()

    -- Apply the update
    changes:update(updated_entry)

    -- Apply changes to create a new version
    local version, err = changes:apply()
    if not version then
        return {
            success = false,
            error = "Failed to apply registry changes: " .. (err or "unknown error")
        }
    end

    -- Return success response
    return {
        success = true,
        message = "Page updated successfully",
        page = {
            id = entry.id.name,
            full_id = entry.id,
            name = updated_entry.meta.name,
            title = updated_entry.meta.title,
            icon = updated_entry.meta.icon,
            version = version:id()
        }
    }
end

return {
    handler = handler
}
