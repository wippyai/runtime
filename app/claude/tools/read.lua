local fs = require("fs")

-- Helper function to get file extension
local function get_file_extension(filename)
    return filename:match("%.([^%.]+)$")
end

-- Helper function to get human-readable file size (missing in original file)
local function format_size(bytes)
    if bytes < 1024 then
        return string.format("%d B", bytes)
    elseif bytes < 1024 * 1024 then
        return string.format("%.2f KB", bytes / 1024)
    elseif bytes < 1024 * 1024 * 1024 then
        return string.format("%.2f MB", bytes / (1024 * 1024))
    else
        return string.format("%.2f GB", bytes / (1024 * 1024 * 1024))
    end
end

function handle(args)
    -- Get parameters from args
    local filepath = args.path
    if not filepath then
        return {error = "Missing required 'path' parameter"}
    end

    local fs_name = args.fs or "system:core"
    local max_size = args.max_size or 10 * 1024 * 1024 -- 10MB default limit

    -- Get the filesystem
    local myfs = fs.get(fs_name)
    if not myfs then
        return {error = "Failed to get filesystem: " .. fs_name}
    end

    -- Check if file exists and is a file
    local stat, err = myfs:stat(filepath)
    if not stat then
        return {error = "File not found: " .. filepath}
    end

    if not stat.is_file then
        return {error = "Path is not a file: " .. filepath}
    end

    -- Check file size limit
    if stat.size > max_size then
        return {error = string.format("File too large (%.2fMB), max size is %.2fMB",
            stat.size / (1024 * 1024), max_size / (1024 * 1024))}
    end

    -- Read file content
    local content = myfs:readfile(filepath)
    if not content then
        return {error = "Failed to read file: " .. filepath}
    end

    print("File read successfully:", filepath, "Size:", format_size(stat.size))

    -- Return the content with file metadata
    return {
        content = content,
        size = stat.size,
        path = filepath,
        modified = stat.modified,
        extension = get_file_extension(filepath)
    }
end

return {
    handle = handle
}