local fs = require("fs")
local json = require("json")

-- Helper function to get file extension
local function get_file_extension(filename)
    return filename:match("%.([^%.]+)$")
end

-- Check if file should be included based on extension
local function should_include_file(filename, extensions)
    if not extensions or #extensions == 0 then
        return true
    end

    local ext = get_file_extension(filename)
    if not ext then
        return false
    end

    ext = ext:lower()
    for _, allowed_ext in ipairs(extensions) do
        if ext == allowed_ext:lower() then
            return true
        end
    end

    return false
end

-- Check if directory should be excluded
local function should_exclude_dir(dirname, excluded_dirs)
    if not excluded_dirs or #excluded_dirs == 0 then
        return false
    end

    for _, excluded in ipairs(excluded_dirs) do
        if dirname == excluded then
            return true
        end
    end

    return false
end

-- Build file tree as a text representation
local function build_file_tree_text(filesystem, path, prefix, excluded_dirs, extensions, max_depth, current_depth)
    if max_depth and current_depth > max_depth then
        return prefix .. "...\n"
    end

    local result = ""
    local entries = {}

    -- Read directory entries
    for entry in filesystem:readdir(path) do
        table.insert(entries, entry)
    end

    -- Sort entries: directories first, then files
    table.sort(entries, function(a, b)
        if a.type == b.type then
            return a.name < b.name
        end
        return a.type == fs.type.DIR
    end)

    -- Process each entry
    for i, entry in ipairs(entries) do
        local is_last = (i == #entries)
        local current_prefix = prefix .. (is_last and "└── " or "├── ")
        local next_prefix = prefix .. (is_last and "    " or "│   ")

        if entry.type == fs.type.DIR then
            if not should_exclude_dir(entry.name, excluded_dirs) then
                result = result .. current_prefix .. entry.name .. "/\n"

                -- Process subdirectory
                local full_path
                if path == "/" then
                    full_path = "/" .. entry.name
                else
                    full_path = path .. "/" .. entry.name
                end

                result = result .. build_file_tree_text(
                    filesystem, full_path, next_prefix, excluded_dirs,
                    extensions, max_depth, current_depth + 1
                )
            end
        elseif entry.type == fs.type.FILE then
            if should_include_file(entry.name, extensions) then
                result = result .. current_prefix .. entry.name .. "\n"
            end
        end
    end

    return result
end

function handle(args)
    -- Get parameters from args
    local root_path = args.path or "/"
    local core_fs_name = args.fs or "system:core"
    local max_depth = args.max_depth
    local format = args.format or "text"

    -- Parse extensions (string or table)
    local extensions = {}
    if type(args.extensions) == "string" then
        for ext in args.extensions:gmatch("([^,]+)") do
            table.insert(extensions, ext:match("^%s*(.-)%s*$")) -- Trim whitespace
        end
    elseif type(args.extensions) == "table" then
        extensions = args.extensions
    end

    -- Parse excluded directories (string or table)
    local excluded_dirs = {}
    if type(args.exclude_dirs) == "string" then
        for dir in args.exclude_dirs:gmatch("([^,]+)") do
            table.insert(excluded_dirs, dir:match("^%s*(.-)%s*$")) -- Trim whitespace
        end
    elseif type(args.exclude_dirs) == "table" then
        excluded_dirs = args.exclude_dirs
    else
        -- Default exclusions
        excluded_dirs = {".git", "node_modules", "build", "dist"}
    end

    -- Get the filesystem
    local core_fs = fs.get(core_fs_name)
    if not core_fs then
        return nil, "Failed to get filesystem: " .. core_fs_name
    end

    -- Check if path exists
    if not core_fs:exists(root_path) then
        return nil, "Path not found: " .. root_path
    end

    -- Generate file tree
    local tree_text = ""
    if root_path == "/" then
        tree_text = "/\n"
    else
        tree_text = root_path .. "\n"
    end
    tree_text = tree_text .. build_file_tree_text(core_fs, root_path, "", excluded_dirs, extensions, max_depth, 1)

    -- Return based on format
    if format == "json" then
        return {
            path = root_path,
            filesystem = core_fs_name,
            extensions = extensions,
            excluded_dirs = excluded_dirs,
            tree = tree_text
        }
    else
        return tree_text
    end
end

return {
    handle = handle
}