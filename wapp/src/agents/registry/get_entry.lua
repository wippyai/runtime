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

    -- Get the entry from registry
    local entry, err = snapshot:get(params.id)

    if not entry then
        return {
            success = false,
            error = "Entry not found: " .. params.id
        }
    end

    -- Build a response with detailed entry information
    local result = {
        id = entry.id,
        kind = entry.kind,
        meta = entry.meta or {},
        data = entry.data or {}
    }

    -- Get the version of the snapshot
    local version = snapshot:version()
    local version_info = nil
    if version then
        version_info = {
            id = version:id(),
            previous = version:previous() and version:previous():id() or nil,
            string = version:string()
        }
    end

    -- Return success response
    return {
        success = true,
        entry = result,
        version = version_info
    }
end

return {
    handler = handler
}
