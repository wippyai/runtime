local fs = require("fs")
local json = require("json")

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

-- Function to read a single file
local function read_single_file(myfs, filepath, max_size)
    -- Check if file exists and is a file
    local stat, err = myfs:stat(filepath)
    if not stat then
        return {error = "File not found: " .. filepath}
    end

    if stat.is_dir then
        return {error = "Path is a directory: " .. filepath}
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

    -- Return the content with file metadata
    return {
        content = content,
        size = stat.size,
        size_human = format_size(stat.size),
        path = filepath,
        modified = stat.modified,
        extension = get_file_extension(filepath)
    }
end

function handle(args)
    -- Get parameters from args
    local filepaths = args.paths
    if not filepaths or type(filepaths) ~= "table" or #filepaths == 0 then
        return {error = "Missing required 'paths' parameter (must be an array of file paths)"}
    end

    local fs_name = args.fs or "system:core"
    local max_size = args.max_size or 10 * 1024 * 1024 -- 10MB default limit
    local format = args.format or "json" -- "json" or "text"

    -- Get the filesystem
    local myfs = fs.get(fs_name)
    if not myfs then
        return {error = "Failed to get filesystem: " .. fs_name}
    end

    -- Read each file
    local results = {}
    local failed = {}
    local total_size = 0

    for _, filepath in ipairs(filepaths) do
        local result = read_single_file(myfs, filepath, max_size)

        if result.error then
            table.insert(failed, {path = filepath, error = result.error})
        else
            table.insert(results, result)
            total_size = total_size + result.size
            print("File read successfully:", filepath, "Size:", result.size_human)
        end
    end

    -- Format and return the results
    if format == "json" then
        return {
            files = results,
            failed = failed,
            count = {
                success = #results,
                failed = #failed,
                total = #filepaths
            },
            total_size = total_size,
            total_size_human = format_size(total_size)
        }
    else
        -- Text format
        local output = string.format("Read %d of %d files (total size: %s)\n\n",
            #results, #filepaths, format_size(total_size))

        if #results > 0 then
            output = output .. "Successfully read files:\n"
            for _, result in ipairs(results) do
                output = output .. string.format("- %s (%s)\n",
                    result.path, result.size_human)
            end
            output = output .. "\n"
        end

        if #failed > 0 then
            output = output .. "Failed to read files:\n"
            for _, failure in ipairs(failed) do
                output = output .. string.format("- %s: %s\n",
                    failure.path, failure.error)
            end
        end

        return output
    end
end

return {
    handle = handle
}