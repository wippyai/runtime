-- List directory contents or generate a tree view
-- @param params Table containing:
--   path (string, optional): Directory path to list (defaults to current directory)
--   recursive (boolean, optional): Whether to generate a tree view (defaults to false)
--   max_depth (number, optional): Maximum depth for recursive listing (defaults to 3)
-- @return Table containing:
--   entries (array of tables): List of directory entries (when recursive=false)
--   tree (string): Formatted tree structure (when recursive=true)
--   path (string): The path that was listed
--   success (boolean): Whether the operation was successful
--   error (string, optional): Error message if operation failed
local fs = require("fs")

local function list(params)
    -- Helper function to format file size
    local function format_size(size)
        local units = {"B", "KB", "MB", "GB", "TB"}
        local unit_index = 1

        while size >= 1024 and unit_index < #units do
            size = size / 1024
            unit_index = unit_index + 1
        end

        return string.format("%.2f %s", size, units[unit_index])
    end

    -- Helper function to build tree structure recursively
    local function build_tree(fs_instance, path, prefix, is_last, max_depth, current_depth)
        if current_depth > max_depth then
            return prefix .. "│  └─ ... (max depth reached)\n"
        end

        local result = ""
        local fs_type = fs.type

        -- Check if directory exists
        if not fs_instance:exists(path) or not fs_instance:isdir(path) then
            return result
        end

        -- Get directory entries and sort them
        local entries = {}
        for entry in fs_instance:readdir(path) do
            table.insert(entries, entry)
        end

        table.sort(entries, function(a, b)
            -- Directories first, then files, sorted alphabetically
            if a.type == fs_type.DIR and b.type ~= fs_type.DIR then
                return true
            elseif a.type ~= fs_type.DIR and b.type == fs_type.DIR then
                return false
            else
                return a.name < b.name
            end
        end)

        -- Process entries
        for i, entry in ipairs(entries) do
            local is_entry_last = (i == #entries)
            local entry_path = path .. "/" .. entry.name
            local entry_prefix = prefix

            -- Get stats
            local stat = fs_instance:stat(entry_path)
            local entry_size = ""

            if entry.type == fs_type.FILE then
                entry_size = " (" .. format_size(stat.size) .. ")"
            end

            -- Draw the appropriate tree characters
            if is_last then
                entry_prefix = entry_prefix .. "    "
            else
                entry_prefix = entry_prefix .. "│   "
            end

            if is_entry_last then
                result = result .. prefix .. "└── " .. entry.name .. entry_size .. "\n"
            else
                result = result .. prefix .. "├── " .. entry.name .. entry_size .. "\n"
            end

            -- Recursively process subdirectories
            if entry.type == fs_type.DIR then
                result = result .. build_tree(
                    fs_instance,
                    entry_path,
                    entry_prefix,
                    is_entry_last,
                    max_depth,
                    current_depth + 1
                )
            end
        end

        return result
    end

    -- Get filesystem instance - hardcoded to app:core
    local fs_instance = fs.get("app:core")
    local fs_type = fs.type

    -- Set defaults
    local path = params.path or fs_instance:pwd()
    local recursive = params.recursive or false
    local max_depth = params.max_depth or 3

    -- Check if directory exists
    if not fs_instance:exists(path) then
        return {
            success = false,
            error = "Directory does not exist: " .. path
        }
    end

    -- Check if path is a directory
    if not fs_instance:isdir(path) then
        return {
            success = false,
            error = "Path exists but is not a directory: " .. path
        }
    end

    -- Handle non-recursive listing
    if not recursive then
        local entries = {}

        for entry in fs_instance:readdir(path) do
            local entry_path = path .. "/" .. entry.name
            local stat = fs_instance:stat(entry_path)

            table.insert(entries, {
                name = entry.name,
                type = entry.type,
                is_dir = (entry.type == fs_type.DIR),
                size = stat.size,
                formatted_size = format_size(stat.size),
                modified = stat.modified
            })
        end

        -- Sort entries: directories first, then files, alphabetically
        table.sort(entries, function(a, b)
            if a.is_dir and not b.is_dir then
                return true
            elseif not a.is_dir and b.is_dir then
                return false
            else
                return a.name < b.name
            end
        end)

        return {
            success = true,
            path = path,
            entries = entries
        }
    end

    -- Handle recursive tree view
    local tree = path .. "\n" .. build_tree(fs_instance, path, "", true, max_depth, 1)

    return {
        success = true,
        path = path,
        tree = tree
    }
end

return list