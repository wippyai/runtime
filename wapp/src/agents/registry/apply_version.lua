local json = require("json")
local registry = require("registry")

local function handler(params)
    -- Validate input
    if not params.version_id then
        return {
            success = false,
            error = "Missing required parameter: version_id"
        }
    end

    -- Ensure version_id is a number
    local version_id = tonumber(params.version_id)
    if not version_id then
        return {
            success = false,
            error = "Invalid version_id: must be a number"
        }
    end

    -- Get registry history
    local history, err = registry.history()
    if not history then
        return {
            success = false,
            error = "Failed to get registry history: " .. (err or "unknown error")
        }
    end

    -- Get the specified version
    local version, err = history:get_version(version_id)
    if not version then
        return {
            success = false,
            error = "Failed to get version " .. version_id .. ": " .. (err or "version not found")
        }
    end

    -- Get current version for comparison
    local current_version = registry.current_version()
    if current_version and current_version:id() == version_id then
        return {
            success = false,
            error = "Already at version " .. version_id
        }
    end

    -- Apply the specified version (rollback)
    local success, err = registry.apply_version(version)
    if not success then
        return {
            success = false,
            error = "Failed to apply version " .. version_id .. ": " .. (err or "unknown error")
        }
    end

    -- Return success response
    return {
        success = true,
        message = "Successfully applied version " .. version_id,
        previous_version = current_version and current_version:id() or nil,
        current_version = version_id
    }
end

return {
    handler = handler
}