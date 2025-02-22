local http = require("http")
local lfs = require("lfs")
local json = require("json")

function list_directory()
    -- Get response object
    local res = http.response()
    if not res then
        return nil, "Failed to get HTTP response"
    end

    -- Set up response
    res:set_content_type(http.CONTENT.JSON)

    -- Get current directory
    local current_dir = lfs.currentdir()
    if not current_dir then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            error = "Failed to get current directory"
        })
        return
    end

    -- List all files and directories
    local entries = {}
    for entry in lfs.dir(current_dir) do
        -- Skip . and .. entries
        if entry ~= "." and entry ~= ".." then
            local attr = lfs.attributes(entry)
            if attr then
                table.insert(entries, {
                    name = entry,
                    type = attr.mode,
                    size = attr.size,
                    modified = attr.modification,
                    is_dir = attr.mode == "directory"
                })
            end
        end
    end

    -- Sort entries (directories first, then files)
    table.sort(entries, function(a, b)
        if a.is_dir and not b.is_dir then return true end
        if not a.is_dir and b.is_dir then return false end
        return a.name:lower() < b.name:lower()
    end)

    -- Send response
    res:set_status(http.STATUS.OK)
    res:write_json({
        current_path = current_dir,
        entries = entries
    })
end

return {
    list_directory = list_directory
}