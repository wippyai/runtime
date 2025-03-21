-- Read the content of a file at the specified path
-- @param params Table containing:
--   path (string): The path to the file to read
-- @return Table containing:
--   content (string): The content of the file
--   size (number): Size of the file in bytes
--   success (boolean): Whether the operation was successful
--   error (string, optional): Error message if operation failed
local fs = require("fs")

local function read(params)
    -- Validate input
    if not params.path then
        return {
            success = false,
            error = "Missing required parameter: path"
        }
    end

    -- Get filesystem instance - hardcoded to system:core
    local fs_instance, err = fs.get("app:core")
    if err then
        return {
            success = false,
            error = "Failed to get filesystem instance: " .. tostring(err)
        }
    end

    -- Check if file exists
    if not fs_instance:exists(params.path) then
        return {
            success = false,
            error = "File does not exist: " .. params.path
        }
    end

    -- Get file stats
    local stat, stat_err = fs_instance:stat(params.path)
    if not stat or stat_err or stat.is_dir then
        return {
            success = false,
            error = "Path exists but is not a file: " .. params.path
        }
    end

    -- Read file content directly without pcall
    local content = fs_instance:readfile(params.path)

    -- Return success with content
    return {
        success = true,
        content = content,
        size = stat.size
    }
end

return read
