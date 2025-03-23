local http = require("http")
local security = require("security")
local fs = require("fs")
local uuid = require("uuid")

local file_repo = require("file_repo")
local file_processor = require("file_processor")

-- File upload handler
local function handler()
    -- Fix 1: Proper error handling for request creation
    local req, err = http.request()
    local res = http.response()

    if err then
        -- Handle request creation error
        if res then
            res:set_status(http.STATUS.INTERNAL_ERROR)
            res:write_json({
                success = false,
                error = "Failed to create request context",
                details = err
            })
        end
        return
    end

    if not req or not res then
        return nil, "Failed to get HTTP context"
    end

    -- Set JSON content type for response
    res:set_content_type(http.CONTENT.JSON)

    -- Fix 2: Use the correct method for checking multipart content
    if not req:is_content_type(http.CONTENT.MULTIPART) then
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

    -- Fix 3: Parse multipart form with size limit before accessing files
    local form, err = req:parse_multipart(50 * 1024 * 1024)
    if err then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            success = false,
            error = "Failed to parse form data",
            details = err
        })
        return
    end

    -- Check if we have the file field
    if not form.files or not form.files.file or #form.files.file == 0 then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            success = false,
            error = "No file uploaded",
            details = "Missing 'file' field in form data"
        })
        return
    end

    -- Get the file from the parsed form
    local file_part = form.files.file[1]

    -- Get file details - check if methods exist before calling them
    local filename
    if type(file_part.name) == "function" then
        filename = file_part:name()
    elseif type(file_part.filename) == "function" then
        filename = file_part:filename()
    else
        -- Fallback to a property if it exists
        filename = file_part.filename or file_part.name or "unknown"
    end

    -- Similarly for content type and size
    local mime_type
    if type(file_part.content_type) == "function" then
        mime_type = file_part:content_type() or "application/octet-stream"
    else
        mime_type = file_part.content_type or "application/octet-stream"
    end

    local file_size
    if type(file_part.size) == "function" then
        file_size = file_part:size()
    else
        file_size = file_part.size or 0
    end

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
            details = tostring(err)
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
                details = tostring(err)
            })
            return
        end
    end

    -- Fix 4: Try to get stream method or fall back to reader method
    local stream, err
    if type(file_part.stream) == "function" then
        stream, err = file_part:stream()
    elseif type(file_part.reader) == "function" then
        stream, err = file_part:reader()
    else
        err = "No stream or reader method available on file object"
    end

    if err then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            success = false,
            error = "Failed to create file stream",
            details = tostring(err)
        })
        return
    end

    -- Save the file to the filesystem
    local ok, err = uploads_fs:writefile(file_record.storage_path, stream, "w")
    if not ok then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            success = false,
            error = "Failed to save file",
            details = tostring(err)
        })
        return
    end

    -- Start file processing
    local process_result, err = file_processor.start_processing(file_record.file_id)
    if err then
        -- Still return success but with a warning
        res:set_status(http.STATUS.OK) -- todo: why????
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