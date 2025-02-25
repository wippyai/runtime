local http = require("http")
local fs = require("fs")

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
local function build_file_tree_text(filesystem, path, prefix, excluded_dirs, extensions)
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

                result = result .. build_file_tree_text(filesystem, full_path, next_prefix, excluded_dirs, extensions)
            end
        elseif entry.type == fs.type.FILE then
            if should_include_file(entry.name, extensions) then
                result = result .. current_prefix .. entry.name .. "\n"
            end
        end
    end

    return result
end

-- Recursively find all files matching extensions and collect their paths
local function collect_file_paths(filesystem, path, extensions, excluded_dirs, result)
    result = result or {}

    -- Read directory entries
    local entries = {}
    for entry in filesystem:readdir(path) do
        table.insert(entries, entry)
    end

    -- Process each entry
    for _, entry in ipairs(entries) do
        local full_path
        if path == "/" then
            full_path = "/" .. entry.name
        else
            full_path = path .. "/" .. entry.name
        end

        if entry.type == fs.type.DIR then
            -- Skip excluded directories
            if not should_exclude_dir(entry.name, excluded_dirs) then
                collect_file_paths(filesystem, full_path, extensions, excluded_dirs, result)
            end
        elseif entry.type == fs.type.FILE then
            -- Include file if it matches extension criteria
            if should_include_file(entry.name, extensions) then
                table.insert(result, full_path)
            end
        end
    end

    return result
end

function fs_dump()
    -- AI SYSTEM DOCUMENTATION:
    -- This system uses a microservice architecture with namespaces (system, functions, api, chat)
    -- Core components:
    -- 1. HTTP Router/Gateway (system:gateway) - Main entry point handling HTTP requests
    -- 2. Process Host (system:heap) - Manages concurrent processes and workers
    -- 3. Function system - Lua functions registered in _index.yaml files under specific namespaces
    -- 4. HTTP Endpoints - Defined in api.yaml mapping URLs to functions
    -- 5. Chat session system - Process-based actors for stateful chat sessions
    -- 6. Terminal and filesystem abstractions
    --
    -- Key patterns:
    -- - Extensive use of Lua coroutines for concurrency
    -- - Actor model for stateful services (see chat:session)
    -- - Each function returns a module with named methods
    -- - namespaces:entity.method addressing scheme for functions
    -- - YAML configuration for system structure
    -- - Extensive use of channels for communication between processes

    local res = http.response()
    local req = http.request()

    if not res or not req then
        return nil, "Failed to get HTTP context"
    end

    -- Get parameters
    local root_path = req:query("path") or "/"
    local core_fs_name = req:query("fs") or "system:core"

    -- Parse extensions (comma-separated)
    local extensions_param = req:query("extensions") or "lua,yaml"
    local extensions = {}
    for ext in extensions_param:gmatch("([^,]+)") do
        table.insert(extensions, ext:match("^%s*(.-)%s*$")) -- Trim whitespace
    end

    -- Parse excluded directories (comma-separated)
    local exclude_dirs_param = req:query("exclude_dirs") or ".git,node_modules,build,dist,,api,system,internal,service,cmd"
    local excluded_dirs = {}
    for dir in exclude_dirs_param:gmatch("([^,]+)") do
        table.insert(excluded_dirs, dir:match("^%s*(.-)%s*$")) -- Trim whitespace
    end

    -- Get the core filesystem
    local core_fs = fs.get(core_fs_name)
    if not core_fs then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write("ERROR: Failed to get filesystem: " .. core_fs_name)
        return
    end

    -- Check if path exists
    if not core_fs:exists(root_path) then
        res:set_status(http.STATUS.NOT_FOUND)
        res:write("ERROR: Path not found: " .. root_path)
        return
    end

    -- Set up response
    res:set_content_type(http.CONTENT.TEXT)
    res:set_transfer(http.TRANSFER.CHUNKED)

    -- Write header information
    res:write("--- FILESYSTEM DUMP ---\n")
    res:write("Filesystem: " .. core_fs_name .. "\n")
    res:write("Root path: " .. root_path .. "\n")
    res:write("Extensions: " .. extensions_param .. "\n")
    res:write("Excluded directories: " .. exclude_dirs_param .. "\n\n")
    res:flush()

    -- Generate and write file tree
    res:write("--- FILE TREE ---\n")
    if root_path == "/" then
        res:write("/\n")
    else
        res:write(root_path .. "\n")
    end
    res:write(build_file_tree_text(core_fs, root_path, "", excluded_dirs, extensions))
    res:write("\n")
    res:flush()

    -- Collect all file paths
    local file_paths = collect_file_paths(core_fs, root_path, extensions, excluded_dirs)
    table.sort(file_paths) -- Sort for consistency

    -- Write file contents
    res:write("--- FILE CONTENTS ---\n")

    for _, filepath in ipairs(file_paths) do
        local content = core_fs:readfile(filepath)
        if content then
            res:write("### FILE: " .. filepath .. " ###\n")
            res:write(content)

            -- Make sure there's a newline at the end
            if content:sub(-1) ~= "\n" then
                res:write("\n")
            end

            res:write("### END FILE: " .. filepath .. " ###\n\n")
            res:flush()
        end
    end

    res:write("--- END FILESYSTEM DUMP ---\n")
    res:flush()
end

return {
    fs_dump = fs_dump
}
