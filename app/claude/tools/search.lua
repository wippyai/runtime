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

-- Helper function to search in a file
local function search_in_file(filesystem, filepath, pattern, case_sensitive)
    local content = filesystem:readfile(filepath)
    if not content then
        return nil
    end

    -- Prepare pattern based on case sensitivity
    local search_pattern
    if case_sensitive then
        search_pattern = pattern
    else
        -- Convert to lowercase for case-insensitive search
        content = content:lower()
        search_pattern = pattern:lower()
    end

    local matches = {}
    local line_num = 1
    local prev_line_end = 0
    local current_pos = 1

    -- Search line by line to get line numbers
    while current_pos do
        local line_end = content:find("\n", current_pos)
        local line

        if line_end then
            line = content:sub(current_pos, line_end - 1)
            prev_line_end = line_end
            current_pos = line_end + 1
        else
            line = content:sub(current_pos)
            current_pos = nil
        end

        -- Search for pattern in the current line
        local start_pos = line:find(search_pattern, 1, true)
        if start_pos then
            local match = {
                line_number = line_num,
                line = content:sub(prev_line_end - #line, prev_line_end - 1)
            }
            table.insert(matches, match)
        end

        line_num = line_num + 1
    end

    return matches
end

-- Recursive function to search in directory
local function search_in_dir(filesystem, root_path, pattern, case_sensitive, extensions, excluded_dirs, results)
    local entries = {}

    -- Read directory entries
    for entry in filesystem:readdir(root_path) do
        table.insert(entries, entry)
    end

    -- Process each entry
    for _, entry in ipairs(entries) do
        local full_path
        if root_path == "/" then
            full_path = "/" .. entry.name
        else
            full_path = root_path .. "/" .. entry.name
        end

        if entry.type == fs.type.DIR then
            -- Skip excluded directories
            if not should_exclude_dir(entry.name, excluded_dirs) then
                search_in_dir(filesystem, full_path, pattern, case_sensitive, extensions, excluded_dirs, results)
            end
        elseif entry.type == fs.type.FILE then
            -- Search in files with matching extensions
            if should_include_file(entry.name, extensions) then
                local file_matches = search_in_file(filesystem, full_path, pattern, case_sensitive)
                if file_matches and #file_matches > 0 then
                    results[full_path] = file_matches
                end
            end
        end
    end
end

function handle(args)
    -- Get parameters from args
    local search_path = args.path or "/"
    local pattern = args.pattern
    if not pattern then
        return {error = "Missing required 'pattern' parameter"}
    end

    local fs_name = args.fs or "system:core"
    local case_sensitive = args.case_sensitive
    if case_sensitive == nil then
        case_sensitive = false -- Default to case-insensitive
    end

    local format = args.format or "text" -- text or json

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
        excluded_dirs = { ".git", "node_modules", "build", "dist" }
    end

    -- Get the filesystem
    local myfs = fs.get(fs_name)
    if not myfs then
        return {error = "Failed to get filesystem: " .. fs_name}
    end

    -- Check if path exists
    if not myfs:exists(search_path) then
        return {error = "Path not found: " .. search_path}
    end

    -- Perform the search
    local results = {}

    -- Check if it's a file or directory
    local stat = myfs:stat(search_path)
    if stat.is_file then
        -- Search in a single file
        if should_include_file(search_path, extensions) then
            local file_matches = search_in_file(myfs, search_path, pattern, case_sensitive)
            if file_matches and #file_matches > 0 then
                results[search_path] = file_matches
            end
        end
    else
        -- Search in a directory recursively
        search_in_dir(myfs, search_path, pattern, case_sensitive, extensions, excluded_dirs, results)
    end

    -- Format results based on output format
    if format == "json" then
        return {
            pattern = pattern,
            path = search_path,
            case_sensitive = case_sensitive,
            matches = results,
            total_files = (#results > 0) and table.getn(results) or 0
        }
    else
        -- Text format
        local output = string.format("Search results for pattern '%s' in %s\n",
            pattern, search_path)
        output = output .. string.format("Case sensitive: %s\n\n",
            case_sensitive and "Yes" or "No")

        local total_matches = 0
        local files_with_matches = 0

        for file_path, matches in pairs(results) do
            files_with_matches = files_with_matches + 1
            total_matches = total_matches + #matches

            output = output .. string.format("File: %s (%d matches)\n",
                file_path, #matches)

            for _, match in ipairs(matches) do
                output = output .. string.format("  Line %d: %s\n",
                    match.line_number, match.line)
            end

            output = output .. "\n"
        end

        output = output .. string.format("Summary: Found %d matches in %d files\n",
            total_matches, files_with_matches)

        return output
    end
end

return {
    handle = handle
}