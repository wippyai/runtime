local fs = require("fs")
local json = require("json")

-- Helper function to get file extension
local function get_file_extension(filename)
    return filename:match("%.([^%.]+)$")
end

-- Function to analyze a file and extract metadata
local function analyze_file(filesystem, filepath)
    -- Get file stats
    local stat, err = filesystem:stat(filepath)
    if not stat then
        return nil, "Failed to get file stats: " .. (err or "unknown error")
    end

    -- Read file content if it's a file
    local content = nil
    if stat.is_file then
        content = filesystem:readfile(filepath)
        if not content then
            return nil, "Failed to read file content"
        end
    end

    -- Basic metadata
    local metadata = {
        path = filepath,
        name = filepath:match("([^/]+)$") or filepath,
        size = stat.size,
        size_human = format_size(stat.size),
        is_dir = stat.is_dir,
        is_file = stat.is_file,
        modified = stat.modified,
        accessed = stat.accessed,
        created = stat.created,
        extension = get_file_extension(filepath)
    }

    -- If it's a directory, add directory-specific metadata
    if stat.is_dir then
        local entries = {}
        local dirs = 0
        local files = 0

        for entry in filesystem:readdir(filepath) do
            if entry.type == fs.type.DIR then
                dirs = dirs + 1
            else
                files = files + 1
            end
            table.insert(entries, entry.name)
        end

        metadata.entries = entries
        metadata.dir_count = dirs
        metadata.file_count = files
        metadata.total_entries = dirs + files
    else
        -- If it's a file, add file-specific metadata
        metadata.content_preview = #content > 1024
            and content:sub(1, 1024) .. "..."
            or content

        -- Detect if it's a binary file
        local is_binary = false
        for i = 1, math.min(1024, #content) do
            local byte = content:byte(i)
            if byte < 9 or (byte > 13 and byte < 32) or byte == 0 then
                is_binary = true
                break
            end
        end
        metadata.is_binary = is_binary

        -- Count lines if it's a text file
        if not is_binary then
            local lines = 0
            for _ in content:gmatch("\n") do
                lines = lines + 1
            end
            metadata.line_count = lines + 1

            -- Detect file type based on extension
            local ext = metadata.extension and metadata.extension:lower()
            if ext then
                if ext == "lua" then
                    metadata.file_type = "Lua script"
                elseif ext == "js" then
                    metadata.file_type = "JavaScript"
                elseif ext == "html" or ext == "htm" then
                    metadata.file_type = "HTML document"
                elseif ext == "css" then
                    metadata.file_type = "CSS stylesheet"
                elseif ext == "json" then
                    metadata.file_type = "JSON data"
                elseif ext == "yaml" or ext == "yml" then
                    metadata.file_type = "YAML document"
                elseif ext == "md" then
                    metadata.file_type = "Markdown document"
                elseif ext == "txt" then
                    metadata.file_type = "Text file"
                elseif ext == "go" then
                    metadata.file_type = "Go source code"
                elseif ext == "py" then
                    metadata.file_type = "Python script"
                else
                    metadata.file_type = ext:upper() .. " file"
                end
            else
                metadata.file_type = "Unknown"
            end
        else
            metadata.file_type = "Binary file"
        end
    end

    return metadata
end

-- Helper function to get human-readable file size
local function format_size(bytes)
    if not bytes then return "0 B" end

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
        return nil, "Missing required 'path' parameter"
    end

    local fs_name = args.fs or "system:core"
    local format = args.format or "json" -- json or text

    -- Get the filesystem
    local myfs = fs.get(fs_name)
    if not myfs then
        return nil, "Failed to get filesystem: " .. fs_name
    end

    -- Check if path exists
    if not myfs:exists(filepath) then
        return nil, "Path not found: " .. filepath
    end

    -- Analyze the file or directory
    local metadata, err = analyze_file(myfs, filepath)
    if not metadata then
        return nil, "Failed to analyze file: " .. (err or "unknown error")
    end

    -- Format results based on output format
    if format == "json" then
        return metadata
    else
        -- Text format
        local output = string.format("Metadata for: %s\n", filepath)
        output = output .. string.format("Type: %s\n", metadata.is_dir and "Directory" or "File")

        if metadata.is_file then
            output = output .. string.format("Size: %s\n", metadata.size_human)
            output = output .. string.format("File Type: %s\n", metadata.file_type)
            output = output .. string.format("Extension: %s\n", metadata.extension or "none")
            output = output .. string.format("Modified: %s\n", metadata.modified or "unknown")

            if not metadata.is_binary then
                output = output .. string.format("Line Count: %d\n", metadata.line_count)
                output = output .. "\nContent Preview:\n"
                output = output .. "----------------------------------------\n"
                output = output .. metadata.content_preview:sub(1, 500) -- First 500 chars only for preview
                output = output .. "\n----------------------------------------\n"
            else
                output = output .. "Content: Binary data\n"
            end
        else
            output = output .. string.format("Entries: %d (%d directories, %d files)\n",
                metadata.total_entries, metadata.dir_count, metadata.file_count)

            if metadata.entries and #metadata.entries > 0 then
                output = output .. "\nContents:\n"
                for _, entry in ipairs(metadata.entries) do
                    output = output .. "  " .. entry .. "\n"
                end
            end
        end

        return output
    end
end

return {
    handle = handle
}
