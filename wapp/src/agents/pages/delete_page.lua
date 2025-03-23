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

    -- Determine the full ID (handling both short and full ID formats) -- todo; remove
    local full_id
    if string.match(params.id, ":") then
        -- Already a full ID in format "namespace:name"
        full_id = params.id
    else
        -- Short ID format, assume fortress.pages namespace
        full_id = "fortress.pages:" .. params.id
    end

    -- Check if page exists
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

    -- Create a changeset
    local changes = snapshot:changes()

    -- Delete the page
    changes:delete(full_id)

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
        message = "Page deleted successfully",
        id = params.id,
        full_id = full_id,
        version = version:id()
    }
end

return {
    handler = handler
}