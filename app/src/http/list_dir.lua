local http = require("http")
local fs = require("fs")
local json = require("json")
local time = require("time")
local funcs = require("funcs")

-- Helper function to get parent path
local function get_parent_path(path)
    if path == "/" then
        return "/"
    end
    if path:sub(-1) == "/" then
        path = path:sub(1, -2)
    end
    local last_sep = path:match(".*/()")
    if not last_sep then
        return "/"
    end
    return path:sub(1, last_sep - 1)
end

-- Helper function to get file extension
local function get_file_extension(filename)
    return filename:match("%.([^%.]+)$")
end

-- Helper function to check if file has an analyzer
local function get_analyzer_for_file(filename)
    local ext = get_file_extension(filename)
    if not ext then return nil end

    -- Map of file extensions to analyzer functions
    local analyzers = {
        ["go"] = "analyze:go", -- corresponds to the analyze namespace's go function
        ["md"] = "analyze:markdown",
        ["lua"] = "analyze:lua",
        ["html"] = "analyze:html"
    }

    return analyzers[ext:lower()]
end

local function handler()
    local res = http.response()
    local req = http.request()
    if not res or not req then
        return nil, "Failed to get HTTP context"
    end

    local path = req:query("path") or "/"
    local action = req:query("action") or "list"
    local myfs = fs.get("system:core")

    if not myfs then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write("Failed to get default filesystem")
        return
    end

    -- First, check if the path exists and get its stats
    local stat, err = myfs:stat(path)
    if not stat then
        -- If stat fails, try getting the parent directory
        local parent_path = get_parent_path(path)
        local parent_stat = myfs:stat(parent_path)
        if parent_stat and parent_stat.is_dir then
            res:set_status(http.STATUS.TEMPORARY_REDIRECT)
            res:set_header("Location", "?path=" .. parent_path)
            return
        end
        res:set_status(http.STATUS.NOT_FOUND)
        res:write("Path not found: " .. path)
        return
    end

    -- If it's a file, handle file-specific actions
    if not stat.is_dir then
        local parent_path = get_parent_path(path)

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
        elseif action == "analyze" then
            -- Get appropriate analyzer
            local analyzer_func = get_analyzer_for_file(path)
            if not analyzer_func then
                res:set_status(http.STATUS.BAD_REQUEST)
                res:write("No analyzer available for this file type")
                return
            end

            -- Call the analyzer
            local executor = funcs.new()
            local analysis, analyze_err = executor:call(analyzer_func, path)

            if analyze_err then
                res:set_status(http.STATUS.INTERNAL_ERROR)
                res:write("Analysis error: " .. analyze_err)
                return
            end

            -- Display analysis results
            res:set_content_type("text/html")
            res:write(string.format([[
<!DOCTYPE html>
<html>
<head>
    <title>File Analysis: %s</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; }
        .header { margin-bottom: 20px; }
        pre {
            background-color: #f5f5f5;
            padding: 15px;
            border-radius: 4px;
            overflow-x: auto;
        }
        .back-link {
            color: #2c5282;
            text-decoration: none;
            margin-bottom: 20px;
            display: inline-block;
        }
        .back-link:hover { text-decoration: underline; }
    </style>
</head>
<body>
    <a href="?path=%s" class="back-link">← Back to directory</a>
    <div class="header">
        <h1>Analysis Results: %s</h1>
    </div>
    <pre>%s</pre>
</body>
</html>
]],
                path,
                parent_path,
                path:match("[^/]+$"),
                analysis.text or "No analysis results available"
            ))
            return
        else
            -- Redirect to parent directory for file paths without specific action
            res:set_status(http.STATUS.TEMPORARY_REDIRECT)
            res:set_header("Location", "?path=" .. parent_path)
            return
        end
    end

    -- If we get here, we're dealing with a directory
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

    -- Directory listing
    res:set_content_type("text/html")
    res:write(string.format([[
<!DOCTYPE html>
<html>
<head>
    <title>Directory Listing: %s</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; }
        table { width: 100%%; border-collapse: collapse; }
        th, td { padding: 8px; text-align: left; border-bottom: 1px solid #ddd; }
        th { background-color: #f2f2f2; }
        tr:hover { background-color: #f5f5f5; }
        .folder { color: #2c5282; }
        .file { color: #2d3748; }
        .size { color: #718096; }
        a { text-decoration: none; color: inherit; }
        .path { margin-bottom: 20px; padding: 10px; background-color: #f8f9fa; border-radius: 4px; }
        .analyze-btn {
            background-color: #4CAF50;
            color: white;
            padding: 5px 10px;
            border: none;
            border-radius: 4px;
            cursor: pointer;
            margin-left: 10px;
            font-size: 12px;
            text-decoration: none;
            display: inline-block;
        }
        .analyze-btn:hover {
            background-color: #45a049;
        }
    </style>
</head>
<body>
    <div class="path">Current path: %s</div>
    <table>
        <tr>
            <th>Name</th>
            <th>Actions</th>
            <th>Type</th>
            <th>Size</th>
            <th>Modified</th>
        </tr>
]], path, path))

    -- Add parent directory link if not in root
    if path ~= "/" then
        local parent = get_parent_path(path)
        res:write(string.format([[
        <tr>
            <td><a href="?path=%s">.. (Parent Directory)</a></td>
            <td></td>
            <td>DIR</td>
            <td>-</td>
            <td>-</td>
        </tr>
]], parent))
    end

    -- List files and directories
    for _, entry in ipairs(fileList) do
        local filePath
        if path == "/" then
            filePath = "/" .. entry.name
        else
            filePath = path .. "/" .. entry.name
        end

        local statInfo, stat_err = myfs:stat(filePath)
        local size = "-"
        local modified = "-"

        if statInfo and not stat_err then
            if not statInfo.is_dir then
                size = string.format("%.2f KB", statInfo.size / 1024)
            end

            if statInfo.modified then
                local t = time.unix(statInfo.modified, 0)
                if t then
                    modified = t:format(time.DATE_TIME)
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

        -- Only show analyze button if we have an analyzer for this file type
        local analyze_button = ""
        if entry.type ~= fs.type.DIR and get_analyzer_for_file(entry.name) then
            analyze_button = string.format([[
                <a href="?path=%s&action=analyze" class="analyze-btn">Analyze</a>
            ]], filePath)
        end

        res:write(string.format([[
        <tr>
            <td><a class="%s" href="%s">%s</a></td>
            <td>%s</td>
            <td>%s</td>
            <td class="size">%s</td>
            <td>%s</td>
        </tr>
]],
            cssClass,
            link,
            entry.name,
            analyze_button,
            entry.type,
            size,
            modified
        ))
    end

    -- End HTML output
    res:write([[
    </table>
</body>
</html>
]])
end

return {
    handler = handler
}
