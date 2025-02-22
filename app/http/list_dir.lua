local http = require("http")
local fs = require("fs")
local json = require("json")
local time = require("time")

-- Helper function to get parent path
local function get_parent_path(path)
    if path == "/" then
        return "/"
    end

    -- Remove trailing slash if exists
    if path:sub(-1) == "/" then
        path = path:sub(1, -2)
    end

    -- Find last directory separator
    local last_sep = path:match(".*/()")
    if not last_sep then
        return "/"
    end

    -- Return everything up to last separator
    return path:sub(1, last_sep-1)
end

function list_directory()
    local res = http.response()
    local req = http.request()
    if not res or not req then
        return nil, "Failed to get HTTP context"
    end

    local path = req:query("path") or "/"
    local action = req:query("action") or "list"
    local myfs = fs.default()

    if not myfs then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write("Failed to get default filesystem")
        return
    end

    -- Handle file reading
    if action == "read" then
        local content = myfs:readfile(path)
        if not content then
            res:set_status(http.STATUS.NOT_FOUND)
            res:write("File not found")
            return
        end

        -- Try to detect if it's text content
        local is_text = true
        for i = 1, #content do
            local byte = content:byte(i)
            if byte < 32 and byte ~= 9 and byte ~= 10 and byte ~= 13 then
                is_text = false
                break
            end
        end

        if is_text then
            res:set_content_type("text/plain")
            res:write(content)
        else
            res:set_content_type("application/octet-stream")
            res:set_header("Content-Disposition", string.format('attachment; filename="%s"', path:match("[^/]+$")))
            res:write(content)
        end
        return
    end

    -- Directory listing
    res:set_content_type("text/html")

    -- Start HTML output
    res:write([[
<!DOCTYPE html>
<html>
<head>
    <title>Directory Listing: ]] .. path .. [[</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; }
        table { width: 100%; border-collapse: collapse; }
        th, td { padding: 8px; text-align: left; border-bottom: 1px solid #ddd; }
        th { background-color: #f2f2f2; }
        tr:hover { background-color: #f5f5f5; }
        .folder { color: #2c5282; }
        .file { color: #2d3748; }
        .size { color: #718096; }
        a { text-decoration: none; color: inherit; }
        .path { margin-bottom: 20px; padding: 10px; background-color: #f8f9fa; border-radius: 4px; }
    </style>
</head>
<body>
    <div class="path">Current path: ]] .. path .. [[</div>
    <table>
        <tr>
            <th>Name</th>
            <th>Type</th>
            <th>Size</th>
            <th>Modified</th>
        </tr>
]])

    -- Add parent directory link if not in root
    if path ~= "/" then
        local parent = get_parent_path(path)
        res:write(string.format([[
        <tr>
            <td><a href="?path=%s">.. (Parent Directory)</a></td>
            <td>DIR</td>
            <td>-</td>
            <td>-</td>
        </tr>
]], parent))
    end

    local fileList = {}
    for entry in myfs:readdir(path) do
        table.insert(fileList, entry)
    end

    -- Sort entries: directories first, then files
    table.sort(fileList, function(a, b)
        if a.type == b.type then
            return a.name < b.name
        end
        return a.type == fs.type.DIR
    end)

    -- List files and directories
    for _, entry in ipairs(fileList) do
        local filePath
        if path == "/" then
            filePath = "/" .. entry.name
        else
            filePath = path .. "/" .. entry.name
        end

        local statInfo, err = myfs:stat(filePath)
        local size = "-"
        local modified = "-"

        if statInfo and not err then
            if not statInfo.is_dir then
                size = string.format("%.2f KB", statInfo.size / 1024)
            end

            if statInfo.modified then
                local t = time.unix(statInfo.modified, 0)
                if t then
                    modified = t:format(time.DateTime)
                end
            end
        end

        local cssClass = entry.type == fs.type.DIR and "folder" or "file"
        local link
        if entry.type == fs.type.DIR then
            link = string.format("?path=%s", filePath)
        else
            link = string.format("?path=%s&action=read", filePath)
        end

        res:write(string.format([[
        <tr>
            <td><a class="%s" href="%s">%s</a></td>
            <td>%s</td>
            <td class="size">%s</td>
            <td>%s</td>
        </tr>
]], cssClass, link, entry.name, entry.type, size, modified))
    end

    -- End HTML output
    res:write([[
    </table>
</body>
</html>
]])
end

return {
    list_directory = list_directory
}