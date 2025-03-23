local json = require("json")
local registry = require("registry")

local function deep_copy(original)
    local copy
    if type(original) == "table" then
        copy = {}
        for key, value in pairs(original) do
            copy[key] = deep_copy(value)
        end
    else
        copy = original
    end
    return copy
end

local function deep_merge(target, source)
    for key, value in pairs(source) do
        if type(value) == "table" and type(target[key]) == "table" then
            deep_merge(target[key], value)
        else
            target[key] = value
        end
    end
    return target
end

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

    -- Get the entry from registry
    local entry, err = snapshot:get(params.id)

    if not entry then
        return {
            success = false,
            error = "Entry not found: " .. params.id
        }
    end

    -- Create a changeset for updates
    local changes = snapshot:changes()

    -- Create updated entry based on original
    local updated_entry = {
        id = entry.id,
        kind = entry.kind,
        meta = deep_copy(entry.meta) or {},
        data = deep_copy(entry.data) or {}
    }

    -- Update fields if provided
    if params.kind then
        updated_entry.kind = params.kind
    end

    if params.meta then
        if params.merge then
            -- Merge the existing meta with the updates
            updated_entry.meta = deep_merge(updated_entry.meta, params.meta)
        else
            -- Replace the entire meta object
            updated_entry.meta = params.meta
        end
    end

    if params.data then
        if params.merge then
            -- Merge the existing data with the updates
            updated_entry.data = deep_merge(updated_entry.data, params.data)
        else
            -- Replace the entire data object
            updated_entry.data = params.data
        end
    end

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
        message = "Entry updated successfully",
        entry = {
            id = params.id,
            kind = updated_entry.kind
        },
        version = version:id()
    }
end

return {
    handler = handler
}
