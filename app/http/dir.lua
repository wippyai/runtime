local http = require("http")
local fs = require("fs")
local json = require("json")

function list_directory()
    local res = http.response()
    if not res then
        return nil, "Failed to get HTTP response"
    end

    res:set_content_type(http.CONTENT.JSON)

    local myfs = fs.default()
    if not myfs then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({ error = "Failed to get default filesystem" })
        return
    end

    -- Specify the directory to list; here, we use the root directory "/"
    local path = "/"
    local fileList = {}

    for entry in myfs:readdir(path) do
        -- Construct full file path.
        local filePath
        if path == "/" then
            filePath = "/" .. entry.name
        else
            filePath = path .. "/" .. entry.name
        end

        local statInfo, err = myfs:stat(filePath)
        if not statInfo then
            statInfo = { error = err }
        end

        table.insert(fileList, {
            name = entry.name,
            type = entry.type,
            stat = statInfo
        })
    end

    res:set_status(http.STATUS.OK)
    res:write_json({
        path = path,
        files = fileList
    })
end

return {
    list_directory = list_directory
}
