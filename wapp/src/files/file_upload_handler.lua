local http = require("http")
local security = require("security")
local fs = require("fs")
local uuid = require("uuid")

local file_repo = require("file_repo")
local file_processor = require("file_processor")

-- File upload handler
local function handler()
    local req = http.request()
    local res = http.response()

    if not req or not res then
        return nil, "Failed to get HTTP context"
    end

    -- Set JSON content type for response
    res:set_content_type(http.CONTENT.JSON)

    -- Check if request is multipart
    if not req:is_multipart() then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            success = false,
            error = "Request must be multipart/form-data"
        })
        return
    end

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

    -- Get user ID from actor
    local user_id = actor:id()
    if not user_id or user_id == "" then
        res:set_status(http.STATUS.UNAUTHORIZED)
        res:write_json({
            success = false,
            error = "Invalid user ID"
        })
        return
    end

    -- Get the file from the request
    local file_part, err = req:get_file("file")
    if not file_part then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            success = false,
            error = "No file uploaded",
            details = err or "Missing 'file' field in form data"
        })
        return
    end

    -- Get file details
    local filename = file_part:filename()
    local mime_type = file_part:content_type() or "application/octet-stream"
    local file_size = file_part:size()

    -- Validate file
    if file_size <= 0 then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            success = false,
            error = "Empty file"
        })
        return
    end

    -- Get uploads filesystem
    local uploads_fs = fs.get("app:uploads")
    if not uploads_fs then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            success = false,
            error = "Failed to access uploads filesystem"
        })
        return
    end

    -- Create a new file record in the database
    local file_record, err = file_repo.create(user_id, filename, file_size, mime_type)
    if err then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            success = false,
            error = "Failed to create file record",
            details = err
        })
        return
    end

    -- Ensure user directory exists
    local user_dir = user_id
    if not uploads_fs:exists(user_dir) then
        local ok, err = uploads_fs:mkdir(user_dir)
        if err then
            res:set_status(http.STATUS.INTERNAL_ERROR)
            res:write_json({
                success = false,
                error = "Failed to create user directory",
                details = err
            })
            return
        end
    end

    -- Save the file to the filesystem
    local ok, err = uploads_fs:writefile(file_record.storage_path, file_part:reader())
    if err then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            success = false,
            error = "Failed to save file",
            details = err
        })
        return
    end

    -- Start file processing
    local process_result, err = file_processor.start_processing(file_record.file_id)
    if err then
        -- Still return success but with a warning
        res:set_status(http.STATUS.OK)
        res:write_json({
            success = true,
            warning = "File uploaded but processing could not be started: " .. err,
            file = file_record
        })
        return
    end

    -- Return success with file record
    res:set_status(http.STATUS.OK)
    res:write_json({
        success = true,
        message = "File uploaded successfully and processing started",
        file = file_record
    })
end

return {
    handler = handler
}