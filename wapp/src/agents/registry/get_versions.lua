local json = require("json")
local registry = require("registry")

local function handler(params)
    -- Set defaults for optional parameters
    local limit = params.limit or 20

    -- Get registry history
    local history, err = registry.history()
    if not history then
        return {
            success = false,
            error = "Failed to get registry history: " .. (err or "unknown error")
        }
    end

    -- Get all versions
    local versions, err = history:versions()
    if err then
        return {
            success = false,
            error = "Failed to get versions: " .. err
        }
    end

    -- Process the versions into a more usable format
    local result_versions = {}
    local total_count = #versions

    -- Limit the number of versions returned
    local count = math.min(limit, total_count)

    for i = 1, count do
        local version = versions[i]
        local previous = version:previous()

        local version_info = {
            id = version:id(),
            timestamp = version:string(),
            previous_id = previous and previous:id() or nil
        }

        table.insert(result_versions, version_info)
    end

    -- Get current version
    local current_version = registry.current_version()
    local current = nil

    if current_version then
        current = {
            id = current_version:id(),
            timestamp = current_version:string()
        }
    end

    -- Return success response
    return {
        success = true,
        versions = result_versions,
        total = total_count,
        current = current,
        limit = limit,
        has_more = count < total_count
    }
end

return {
    handler = handler
}
