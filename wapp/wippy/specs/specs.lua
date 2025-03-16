local fs = require("fs")
local json = require("json")
local http = require("http")

-- Function to get all .md files in the framework directory
local function get_specs()
    local specs = {}
    local filesystem = fs.get("system:framework")  -- Use the framework filesystem

    -- Recursive function to scan directory and subdirectories
    local function scan_directory(path)
        for entry in filesystem:readdir(path) do
            local entry_path = path
            if path ~= "/" then
                entry_path = path .. "/" .. entry.name
            else
                entry_path = "/" .. entry.name
            end

            if entry.type == fs.type.FILE and entry.name:match("%.md$") then
                local content = filesystem:readfile(entry_path)

                -- Extract title (first line)
                local title = content:match("^# (.-)\n") or entry.name

                -- Remove the "# " prefix if present
                title = title:gsub("^# ", "")

                table.insert(specs, {
                    filename = entry_path,
                    title = title,
                    content = content
                })
            elseif entry.type == fs.type.DIR and entry.name ~= "." and entry.name ~= ".." then
                scan_directory(entry_path)
            end
        end
    end

    -- Start scanning from root
    scan_directory("/")

    return specs
end

-- Endpoint handler function
local function get_all_specs()
    local res = http.response()

    if not res then
        return nil, "Failed to create HTTP response"
    end

    -- Set response headers
    res:set_status(http.STATUS.OK)
    res:set_content_type(http.CONTENT.JSON)
    res:set_header("Access-Control-Allow-Origin", "*")
    res:set_header("Access-Control-Allow-Methods", "GET")

    -- Get all specs
    local specs, err = get_specs()
    if err then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            error = err
        })
        return false
    end

    -- Return the specs as JSON
    res:write_json({
        specs = specs
    })

    return true
end

return { get_all_specs = get_all_specs }