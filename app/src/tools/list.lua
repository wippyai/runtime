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

function handle(args)
    -- Get parameters from args
    local path = args.path or "/"
    local fs_name = args.fs or "system:core"
    local format = args.format or "detailed" -- basic, detailed, json
    local show_hidden = args.show_hidden or false

    -- Get the filesystem
    local myfs = fs.get(fs_name)
    if not myfs then
        return {error = "Failed to get filesystem: " .. fs_name}
    end

    -- Check if path exists and is a directory
    local stat, err = myfs:stat(path)
    if not stat then
        return {error = "Path not found: " .. path}
    end

    if not stat.is_dir then
        return {error = "Path is not a directory: " .. path}
    end

    -- Read directory entries
    local entries = {}
    for entry in myfs:readdir(path) do
        -- Skip hidden files if show_hidden is false
        if show_hidden or not entry.name:match("^%.") then
            table.insert(entries, entry)
        end
    end

    -- Sort entries: directories first, then files
    table.sort(entries, function(a, b)
        if a.type == b.type then
            return a.name < b.name
        end
        return a.type == fs.type.DIR
    end)

    -- Process entries based on format
    if format == "basic" then
        -- Just return names
        local names = {}
        for _, entry in ipairs(entries) do
            if entry.type == fs.type.DIR then
                table.insert(names, entry.name .. "/")
            else
                table.insert(names, entry.name)
            end
        end
        return names
    elseif format == "json" or format == "detailed" then
        -- Return detailed information
        local result = {
            path = path,
            fs = fs_name,
            entries = {}
        }

        for _, entry in ipairs(entries) do
            local full_path
            if path == "/" then
                full_path = "/" .. entry.name
            else
                full_path = path .. "/" .. entry.name
            end

            local entry_stat = myfs:stat(full_path)
            local entry_info = {
                name = entry.name,
                type = entry.type == fs.type.DIR and "directory" or "file",
                path = full_path
            }

            if entry_stat then
                entry_info.size = entry_stat.size
                entry_info.size_human = format_size(entry_stat.size)
                entry_info.modified = entry_stat.modified

                if entry.type == fs.type.FILE then
                    entry_info.extension = get_file_extension(entry.name)
                end
            end

            table.insert(result.entries, entry_info)
        end

        if format == "json" then
            return result
        else
            -- Format as text for detailed view
            local output = string.format("Directory: %s\n\n", path)
            output = output .. string.format("%-30s %-12s %-12s %s\n", "Name", "Type", "Size", "Modified")
            output = output .. string.rep("-", 70) .. "\n"

            for _, entry in ipairs(result.entries) do
                local name = entry.name
                if entry.type == "directory" then
                    name = name .. "/"
                end

                local size = entry.size_human or "-"
                local modified = entry.modified or "-"

                output = output .. string.format("%-30s %-12s %-12s %s\n",
                    name:sub(1, 30),
                    entry.type,
                    size,
                    modified
                )
            end

            return output
        end
    else
        return {error = "Unknown format: " .. format}
    end
end

return {
    handle = handle
}