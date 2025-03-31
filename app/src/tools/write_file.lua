local fs = require("fs")

-- Helper function to get file extension
local function get_file_extension(filename)
    return filename:match("%.([^%.]+)$")
end

-- Helper function to get human-readable file size
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

    local content = args.content
    if not content then
        return {error = "Missing required 'content' parameter"}
    end

    local fs_name = args.fs or "system:core"
    local mode = args.mode or "w" -- Default to write/truncate

    -- Validate mode
    if mode ~= "w" and mode ~= "wx" and mode ~= "a" then
        return {error = "Invalid mode. Must be one of: 'w', 'wx', 'a'"}
    end

    -- Get the filesystem
    local myfs = fs.get(fs_name)
    if not myfs then
        return {error = "Failed to get filesystem: " .. fs_name}
    end

    -- Check for "wx" mode (fail if file exists)
    if mode == "wx" and myfs:exists(filepath) then
        return {error = "File already exists: " .. filepath}
    end

    -- Create parent directories if they don't exist
    local dir_path = filepath:match("(.*)/[^/]+$")
    if dir_path and not myfs:exists(dir_path) then
        local success, err = pcall(function() myfs:mkdir(dir_path) end)
        if not success then
            return {error = "Failed to create parent directory: " .. err}
        end
    end

    -- Write the content to the file
    local success, err = pcall(function()
        myfs:writefile(filepath, content, mode)
    end)

    if not success then
        return {error = "Failed to write file: " .. err}
    end

    -- Get file stats after writing
    local stat = myfs:stat(filepath)

    print("File written successfully:", filepath, "Size:", format_size(stat.size))

    -- Return success with file metadata
    return {
        success = true,
        path = filepath,
        size = stat.size,
        size_human = format_size(stat.size),
        modified = stat.modified,
        extension = get_file_extension(filepath)
    }
end

return {
    handle = handle
}