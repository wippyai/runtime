local fs = require("fs")

-- Helper function to get file extension
local function get_file_extension(filename)
    return filename:match("%.([^%.]+)$")
end

-- Helper function to detect if content is binary
local function is_binary(content)
    -- Check first 1024 bytes for null bytes or other non-text characters
    local check_length = math.min(1024, #content)
    for i = 1, check_length do
        local byte = content:byte(i)
        -- Exclude common control characters that can appear in text
        if byte < 9 or (byte > 13 and byte < 32) or byte == 0 then
            return true
        end
    end
    return false
end

function handle(args)
    -- Get parameters from args
    local filepath = args.path
    if not filepath then
        return nil, "Missing required 'path' parameter"
    end

    local fs_name = args.fs or "system:core"
    local max_size = args.max_size or 10 * 1024 * 1024 -- 10MB default limit
    local binary_ok = args.binary_ok or false -- Whether to return binary content

    -- Get the filesystem
    local myfs = fs.get(fs_name)
    if not myfs then
        return nil, "Failed to get filesystem: " .. fs_name
    end

    -- Check if file exists and is a file
    local stat, err = myfs:stat(filepath)
    if not stat then
        return nil, "File not found: " .. filepath
    end

    if not stat.is_file then
        return nil, "Path is not a file: " .. filepath
    end

    -- Check file size limit
    if stat.size > max_size then
        return nil, string.format("File too large (%.2fMB), max size is %.2fMB",
            stat.size / (1024 * 1024), max_size / (1024 * 1024))
    end

    -- Read file content
    local content = myfs:readfile(filepath)
    if not content then
        return nil, "Failed to read file: " .. filepath
    end

    -- Check if binary and handle accordingly
    if is_binary(content) and not binary_ok then
        return nil, "File contains binary data. Set binary_ok=true to read anyway."
    end

    -- Return the content with file metadata
    return {
        content = content,
        size = stat.size,
        path = filepath,
        modified = stat.modified,
        is_binary = is_binary(content),
        extension = get_file_extension(filepath)
    }
end

return {
    handle = handle
}