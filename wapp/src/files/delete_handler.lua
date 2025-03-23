local http = require("http")
local security = require("security")
local fs = require("fs")

local file_repo = require("file_repo")

-- Delete file handler
local function delete_handler()
    local req = http.request()
    local res = http.response()

    if not req or not res then
        return nil, "Failed to get HTTP context"
    end

    -- Set JSON content type for response
    res:set_content_type(http.CONTENT.JSON)

    -- Get current user from security context
    local actor = security.actor()
    if not actor then
        res:set_status(http.STATUS.UNAUTHORIZED)
        res:write_json({
            success = false,
            error = "Authentication required"
        })
        return
    end

    -- Parse URL parameters
    local params = req:params()
    local file_id = params["file_id"]

    if not file_id or file_id == "" then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            success = false,
            error = "File ID is required"
        })
        return
    end

    -- Get file information first to check ownership
    local file, err = file_repo.get(file_id)
    if err then
        res:set_status(http.STATUS.NOT_FOUND)
        res:write_json({
            success = false,
            error = "File not found",
            details = err
        })
        return
    end

    -- Check if the file belongs to the current user
    if file.user_id ~= actor:id() then
        res:set_status(http.STATUS.FORBIDDEN)
        res:write_json({
            success = false,
            error = "You do not have permission to delete this file"
        })
        return
    end

    -- Get uploads filesystem to delete the physical file
    local uploads_fs = fs.get("app:uploads")
    if uploads_fs and uploads_fs:exists(file.storage_path) then
        -- Use pcall to handle potential errors during removal
        local success, error_msg = pcall(function()
            uploads_fs:remove(file.storage_path)
        end)

        if not success then
            print("Warning: Failed to remove physical file: " .. error_msg)
            -- Continue with database deletion even if physical file deletion fails
        end
    end

    -- Delete file from database
    local result, err = file_repo.delete(file_id)
    if err then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            success = false,
            error = "Failed to delete file",
            details = err
        })
        return
    end

    -- Return success
    res:set_status(http.STATUS.OK)
    res:write_json({
        success = true,
        message = "File deleted successfully"
    })
end

return {
    delete_handler = delete_handler
}